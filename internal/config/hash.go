package config

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// hashable is the normalized form of a Container used for hashing.
// Disabled is excluded — toggling it must not trigger a recreate.
type hashable struct {
	Image         string            `json:"image"`
	Command       []string          `json:"command,omitempty"`
	Entrypoint    []string          `json:"entrypoint,omitempty"`
	Restart       string            `json:"restart"`
	Ports         []string          `json:"ports,omitempty"`
	Volumes       []string          `json:"volumes,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	Networks      []string          `json:"networks,omitempty"`
	CPUs          string            `json:"cpus,omitempty"`
	Memory        string            `json:"memory,omitempty"`
	PidsLimit     int64             `json:"pids_limit,omitempty"`
	Healthcheck   *hashHealthcheck  `json:"healthcheck,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	User          string            `json:"user,omitempty"`
	WorkingDir    string            `json:"working_dir,omitempty"`
	Hostname      string            `json:"hostname,omitempty"`
	DNS           []string          `json:"dns,omitempty"`
	CapAdd        []string          `json:"cap_add,omitempty"`
	CapDrop       []string          `json:"cap_drop,omitempty"`
	Privileged    bool              `json:"privileged,omitempty"`
	ReadOnly      bool              `json:"read_only,omitempty"`
	Tmpfs         []string          `json:"tmpfs,omitempty"`
	DependsOn     []string          `json:"depends_on,omitempty"`
}

type hashHealthcheck struct {
	Test     []string `json:"test,omitempty"`
	Interval string   `json:"interval,omitempty"`
	Timeout  string   `json:"timeout,omitempty"`
	Retries  int      `json:"retries,omitempty"`
	Start    string   `json:"start_period,omitempty"`
}

func Hash(c *Container) string {
	h := normalize(c)
	b, _ := json.Marshal(h)
	sum := sha256.Sum256(b)
	return fmt.Sprintf("sha256:%x", sum)
}

func normalize(c *Container) hashable {
	h := hashable{
		Image:      c.Image,
		Command:    c.Command,
		Entrypoint: c.Entrypoint,
		Restart:    c.Restart,
		Ports:      c.Ports,
		Volumes:    c.Volumes,
		CPUs:       c.Resources.CPUs,
		Memory:     c.Resources.Memory,
		PidsLimit:  c.Resources.PidsLimit,
		User:       c.User,
		WorkingDir: c.WorkingDir,
		Hostname:   c.Hostname,
		Privileged: c.Privileged,
		ReadOnly:   c.ReadOnly,
		Tmpfs:      c.Tmpfs,
		DependsOn:  c.DependsOn,
	}

	// copy env (already resolved from env_file by Load)
	if len(c.Env) > 0 {
		h.Env = make(map[string]string, len(c.Env))
		for k, v := range c.Env {
			h.Env[k] = v
		}
	}

	// copy labels, excluding any containerctl.* keys
	if len(c.Labels) > 0 {
		h.Labels = make(map[string]string)
		for k, v := range c.Labels {
			if !strings.HasPrefix(k, "containerctl.") {
				h.Labels[k] = v
			}
		}
		if len(h.Labels) == 0 {
			h.Labels = nil
		}
	}

	if c.Healthcheck != nil {
		h.Healthcheck = &hashHealthcheck{
			Test:     c.Healthcheck.Test,
			Interval: c.Healthcheck.Interval,
			Timeout:  c.Healthcheck.Timeout,
			Retries:  c.Healthcheck.Retries,
			Start:    c.Healthcheck.Start,
		}
	}

	// sort slices with no ordering semantics
	if len(h.Networks) > 0 {
		nets := make([]string, len(c.Networks))
		copy(nets, c.Networks)
		sort.Strings(nets)
		h.Networks = nets
	} else {
		h.Networks = c.Networks
	}

	if len(c.DNS) > 0 {
		dns := make([]string, len(c.DNS))
		copy(dns, c.DNS)
		sort.Strings(dns)
		h.DNS = dns
	}
	if len(c.CapAdd) > 0 {
		ca := make([]string, len(c.CapAdd))
		copy(ca, c.CapAdd)
		sort.Strings(ca)
		h.CapAdd = ca
	}
	if len(c.CapDrop) > 0 {
		cd := make([]string, len(c.CapDrop))
		copy(cd, c.CapDrop)
		sort.Strings(cd)
		h.CapDrop = cd
	}

	return h
}

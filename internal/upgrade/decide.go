package upgrade

import (
	"fmt"
	"strings"
)

// Action is what upgrade should do after comparing current vs remote.
type Action int

const (
	// ActionNone: already on the requested build.
	ActionNone Action = iota
	// ActionUpgrade: replace the binary with the remote release.
	ActionUpgrade
	// ActionRefuse: requested tag is not newer than the running binary.
	ActionRefuse
)

// Decision is the result of Decide.
type Decision struct {
	Action Action
	Reason string
}

// Decide compares the running binary to a remote GitHub release.
//
// requested is the user's --version value (empty = stay on the current track:
// devel stays on devel, anything else uses the latest stable release).
// currentDigest is the sha256 hex of the running executable (empty if unknown).
func Decide(current Version, requested string, remote *Release, currentDigest string) Decision {
	if remote == nil {
		return Decision{Action: ActionRefuse, Reason: "no remote release"}
	}
	want := NormalizeTag(requested)
	if want == "" {
		if current.Track == TrackDevel {
			want = "devel"
		} else {
			want = remote.Tag
		}
	}

	if assetDigestMatch(remote, currentDigest) {
		return Decision{
			Action: ActionNone,
			Reason: fmt.Sprintf("already running %s (binary digest matches the release)", displayTag(remote)),
		}
	}

	if want == "devel" {
		return decideDevel(current, remote)
	}
	return decideRelease(current, requested, remote)
}

func decideDevel(current Version, remote *Release) Decision {
	remoteSHA := remote.CommitSHA
	label := displayTag(remote)
	if remoteSHA != "" {
		label = fmt.Sprintf("devel (%s)", shortSHA(remoteSHA))
	}

	if current.Track != TrackDevel {
		return Decision{
			Action: ActionUpgrade,
			Reason: fmt.Sprintf("switch from %s to %s", current.Raw, label),
		}
	}
	if current.DevelSHA != "" && remoteSHA != "" {
		if SameCommit(current.DevelSHA, remoteSHA) {
			return Decision{
				Action: ActionNone,
				Reason: fmt.Sprintf("already at the latest devel build (%s)", shortSHA(remoteSHA)),
			}
		}
		return Decision{
			Action: ActionUpgrade,
			Reason: fmt.Sprintf("new devel build %s (running %s)", shortSHA(remoteSHA), shortSHA(current.DevelSHA)),
		}
	}
	if current.DevelSHA != "" && remoteSHA == "" {
		return Decision{
			Action: ActionRefuse,
			Reason: "cannot confirm a newer devel build: the GitHub release does not include a commit id",
		}
	}
	return Decision{
		Action: ActionRefuse,
		Reason: "cannot confirm a newer devel build: this binary has no commit id (expected devel-<sha>)",
	}
}

func decideRelease(current Version, requested string, remote *Release) Decision {
	target := ParseVersion(remote.Tag)
	if target.Track != TrackRelease {
		target = ParseVersion(requested)
	}
	if target.Track != TrackRelease {
		return Decision{Action: ActionRefuse, Reason: fmt.Sprintf("not a version tag: %s", remote.Tag)}
	}

	switch current.Track {
	case TrackDevel:
		return Decision{
			Action: ActionUpgrade,
			Reason: fmt.Sprintf("switch from %s to %s", current.Raw, remote.Tag),
		}
	case TrackUnknown:
		return Decision{
			Action: ActionUpgrade,
			Reason: fmt.Sprintf("install %s (current build is %s)", remote.Tag, orDev(current.Raw)),
		}
	case TrackRelease:
		if SemverGreater(target.Semver, current.Semver) {
			return Decision{
				Action: ActionUpgrade,
				Reason: fmt.Sprintf("%s is newer than %s", remote.Tag, current.Raw),
			}
		}
		// Auto "latest" that is not newer is already up to date, not an error.
		if strings.TrimSpace(requested) == "" {
			return Decision{
				Action: ActionNone,
				Reason: fmt.Sprintf("already up to date (%s)", current.Raw),
			}
		}
		if semverEqual(target.Semver, current.Semver) && !current.ExtraCommits {
			return Decision{
				Action: ActionNone,
				Reason: fmt.Sprintf("already running %s", remote.Tag),
			}
		}
		return Decision{
			Action: ActionRefuse,
			Reason: fmt.Sprintf("%s is not greater than current version %s", remote.Tag, current.Raw),
		}
	}
	return Decision{Action: ActionRefuse, Reason: "cannot compare versions"}
}

func assetDigestMatch(remote *Release, currentDigest string) bool {
	if currentDigest == "" || remote == nil {
		return false
	}
	want, err := AssetName("", "")
	if err != nil {
		return false
	}
	for _, a := range remote.Assets {
		if a.Name != want {
			continue
		}
		if hex := digestHex(a.Digest); hex != "" && hex == currentDigest {
			return true
		}
	}
	return false
}

func displayTag(r *Release) string {
	if r.Tag != "" {
		return r.Tag
	}
	return r.Name
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func orDev(s string) string {
	if s == "" {
		return "dev"
	}
	return s
}

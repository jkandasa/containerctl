package upgrade

import "testing"

func TestDecideReleaseNewer(t *testing.T) {
	dec := Decide(ParseVersion("v1.2.3"), "v1.2.4", &Release{Tag: "v1.2.4"}, "")
	if dec.Action != ActionUpgrade {
		t.Fatalf("action=%d reason=%s", dec.Action, dec.Reason)
	}
}

func TestDecideReleaseNotGreater(t *testing.T) {
	dec := Decide(ParseVersion("v1.2.3"), "v1.2.2", &Release{Tag: "v1.2.2"}, "")
	if dec.Action != ActionRefuse {
		t.Fatalf("action=%d reason=%s", dec.Action, dec.Reason)
	}
}

func TestDecideReleaseSame(t *testing.T) {
	dec := Decide(ParseVersion("v1.2.3"), "v1.2.3", &Release{Tag: "v1.2.3"}, "")
	if dec.Action != ActionNone {
		t.Fatalf("action=%d reason=%s", dec.Action, dec.Reason)
	}
}

func TestDecideReleaseSameWithExtraCommits(t *testing.T) {
	dec := Decide(ParseVersion("v1.2.3-5-gabcdef1"), "v1.2.3", &Release{Tag: "v1.2.3"}, "")
	if dec.Action != ActionRefuse {
		t.Fatalf("action=%d reason=%s", dec.Action, dec.Reason)
	}
}

func TestDecideLatestAlreadyCurrent(t *testing.T) {
	dec := Decide(ParseVersion("v1.2.3"), "", &Release{Tag: "v1.2.3"}, "")
	if dec.Action != ActionNone {
		t.Fatalf("action=%d reason=%s", dec.Action, dec.Reason)
	}
}

func TestDecideLatestNewer(t *testing.T) {
	dec := Decide(ParseVersion("v1.2.3"), "", &Release{Tag: "v1.2.4"}, "")
	if dec.Action != ActionUpgrade {
		t.Fatalf("action=%d reason=%s", dec.Action, dec.Reason)
	}
}

func TestDecideDevelNewCommit(t *testing.T) {
	dec := Decide(ParseVersion("devel-abc1234"), "devel", &Release{
		Tag:       "devel",
		CommitSHA: "fff9999aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "")
	if dec.Action != ActionUpgrade {
		t.Fatalf("action=%d reason=%s", dec.Action, dec.Reason)
	}
}

func TestDecideDevelSameCommit(t *testing.T) {
	dec := Decide(ParseVersion("devel-abc1234"), "devel", &Release{
		Tag:       "devel",
		CommitSHA: "abc1234bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}, "")
	if dec.Action != ActionNone {
		t.Fatalf("action=%d reason=%s", dec.Action, dec.Reason)
	}
}

func TestDecideDevelCannotConfirm(t *testing.T) {
	dec := Decide(ParseVersion("devel-abc1234"), "devel", &Release{Tag: "devel"}, "")
	if dec.Action != ActionRefuse {
		t.Fatalf("action=%d reason=%s", dec.Action, dec.Reason)
	}
}

func TestDecideSwitchToDevel(t *testing.T) {
	dec := Decide(ParseVersion("v1.2.3"), "devel", &Release{
		Tag:       "devel",
		CommitSHA: "abc1234bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}, "")
	if dec.Action != ActionUpgrade {
		t.Fatalf("action=%d reason=%s", dec.Action, dec.Reason)
	}
}

func TestDecideDevelToRelease(t *testing.T) {
	dec := Decide(ParseVersion("devel-abc1234"), "v1.2.4", &Release{Tag: "v1.2.4"}, "")
	if dec.Action != ActionUpgrade {
		t.Fatalf("action=%d reason=%s", dec.Action, dec.Reason)
	}
}

func TestDecideUnknownToLatest(t *testing.T) {
	dec := Decide(ParseVersion("dev"), "", &Release{Tag: "v1.2.4"}, "")
	if dec.Action != ActionUpgrade {
		t.Fatalf("action=%d reason=%s", dec.Action, dec.Reason)
	}
}

func TestDecideDigestMatchSkips(t *testing.T) {
	name, err := AssetName("", "")
	if err != nil {
		t.Skip(err)
	}
	dec := Decide(ParseVersion("devel-oldold1"), "devel", &Release{
		Tag:       "devel",
		CommitSHA: "fff9999aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Assets: []Asset{{
			Name:   name,
			Digest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		}},
	}, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if dec.Action != ActionNone {
		t.Fatalf("digest match should skip, action=%d reason=%s", dec.Action, dec.Reason)
	}
}

func TestDecideDefaultTrackFollowsDevel(t *testing.T) {
	dec := Decide(ParseVersion("devel-abc1234"), "", &Release{
		Tag:       "devel",
		CommitSHA: "abc1234bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}, "")
	if dec.Action != ActionNone {
		t.Fatalf("auto devel should compare commits, action=%d reason=%s", dec.Action, dec.Reason)
	}
}

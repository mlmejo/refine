package draft

import "testing"

func TestPromptsVersionIsStableAndShort(t *testing.T) {
	a, b := PromptsVersion(), PromptsVersion()
	if a != b || len(a) != 10 {
		t.Fatalf("PromptsVersion = %q then %q", a, b)
	}
	// A change to any rule changes it.
	saved := plainText
	plainText += " "
	defer func() { plainText = saved }()
	if PromptsVersion() == a {
		t.Error("changing a rule did not change the version")
	}
}

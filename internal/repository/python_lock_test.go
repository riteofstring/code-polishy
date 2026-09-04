package repository

import "testing"

func TestParsePythonUVLockUsesCanonicalFacts(t *testing.T) {
	t.Parallel()
	lock, err := ParsePythonUVLock("uv.lock", []byte("version = 1\nrevision = 2\nrequires-python = \"==3.12.*\"\n[[package]]\nname = \"Z_Package\"\nversion = \"2.0\"\nsource = { registry = \"https://pypi.org/simple\" }\n[[package]]\nname = \"a.package\"\nsource = { editable = \".\" }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if lock.Version != 1 || lock.Revision != 2 || lock.RequiresPython != "==3.12.*" || len(lock.Packages) != 2 {
		t.Fatalf("lock = %+v", lock)
	}
	if lock.Packages[0].Name != "z-package" || lock.Packages[1].Name != "a-package" || len(lock.Packages[0].Source) != 1 || lock.Packages[0].Source[0].Name != "registry" {
		t.Fatalf("packages = %+v", lock.Packages)
	}
}

func TestParsePythonUVLockRejectsMalformedFacts(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"[[package]\n",
		"[[package]]\nname = \"example[extra]\"\nsource = { editable = \".\" }\n",
		"[[package]]\nname = \"example\"\nsource = { registry = false }\n",
	} {
		if _, err := ParsePythonUVLock("uv.lock", []byte(source)); err == nil {
			t.Fatalf("malformed uv.lock was accepted: %q", source)
		}
	}
}

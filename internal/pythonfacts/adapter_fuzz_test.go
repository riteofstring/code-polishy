package pythonfacts

import "testing"

func FuzzAdapterBoundary(f *testing.F) {
	python, err := DefaultInterpreter()
	if err != nil {
		f.Fatal(err)
	}
	f.Add("value = 1\n", "Example==1", "version = 1\n[[package]]\nname = \"Example\"\nversion = \"1\"\nsource = { registry = \"https://pypi.org/simple\" }\n")
	f.Add("import importlib\nimportlib.import_module(name)\n", "Name[extra]>=1; python_version >= '3.12'", "[[package]]\nname = \"local\"\nsource = { editable = \".\" }\n")
	f.Add("type Alias[T = int] = list[T]\n", "not a requirement @", "[[package]\n")
	f.Fuzz(func(t *testing.T, source, requirement, lock string) {
		if len(source)+len(requirement)+len(lock) > 64*1024 {
			return
		}
		response, err := Analyze(python, Request{
			Sources:      []Input{{Path: "fuzz.py", Source: source}},
			Locks:        []Input{{Path: "uv.lock", Source: lock}},
			Metadata:     []Input{{Path: "fuzz.dist-info/METADATA", Source: "Metadata-Version: 2.4\nName: fuzz\nVersion: 1\nRequires-Dist: " + requirement + "\n\n"}},
			Requirements: []string{requirement},
		})
		if err != nil {
			return
		}
		if response.Protocol != Protocol || response.PackagingVersion != PackagingVersion ||
			len(response.Sources) != 1 || response.Sources[0].Path != "fuzz.py" ||
			len(response.Locks) != 1 || response.Locks[0].Path != "uv.lock" ||
			len(response.Metadata) != 1 || response.Metadata[0].Path != "fuzz.dist-info/METADATA" ||
			len(response.Requirements) != 1 || response.Requirements[0].Input != requirement {
			t.Fatalf("response = %+v", response)
		}
	})
}

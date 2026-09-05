package pythonfacts

import (
	"bytes"
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestProgramInputRunsLargePolicySourceAndPreservesItsRequest(t *testing.T) {
	source := "import json,sys\nvalue=" + strconv.Quote(strings.Repeat("value-", 10000)) + "\nrequest=json.load(sys.stdin)\njson.dump([len(value),request],sys.stdout,ensure_ascii=False)\n"
	output, err := runFactProject(t.Context(), typeTestInterpreter(t), strings.NewReader("{\"value\":\"π\\nnext\"}\n"), source)
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "[60000, {\"value\": \"π\\nnext\"}]" {
		t.Fatalf("policy program lost its data boundary: %s", output)
	}
}

func TestProgramInputAcceptsTheExactRecordLimit(t *testing.T) {
	source := "print(42)" + strings.Repeat(" ", maximumProgramRecordSize-3-len("print(42)"))
	output, err := runFactProject(t.Context(), typeTestInterpreter(t), strings.NewReader(""), source)
	if err != nil || string(output) != "42\n" {
		t.Fatalf("exact bounded policy program = %q, error = %v", output, err)
	}
	if _, err := ProgramInput(source+" ", strings.NewReader("")); err == nil {
		t.Fatal("oversized encoded policy record was accepted")
	}
}

func TestProgramInputRejectsInvalidPolicySource(t *testing.T) {
	for _, source := range []string{"", "print(42)\x00", string([]byte{0xff}), strings.Repeat("x", maximumProgramRecordSize+1), strings.Repeat("\n", maximumProgramRecordSize/2)} {
		if _, err := ProgramInput(source, strings.NewReader("{}")); err == nil {
			t.Fatalf("invalid policy source accepted: %d bytes", len(source))
		}
	}
}

func TestProgramBootstrapRejectsIncompleteOrOversizedRecordsBeforeExecution(t *testing.T) {
	python := typeTestInterpreter(t)
	encoded, err := json.Marshal("print('executed')")
	if err != nil {
		t.Fatal(err)
	}
	oversized, err := json.Marshal("print('executed')" + strings.Repeat(" ", maximumProgramRecordSize))
	if err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string][]byte{
		"empty": nil, "unterminated": encoded, "truncated": encoded[:len(encoded)-1],
		"oversized": append(oversized, '\n'), "invalid JSON": []byte("print('executed')\n"), "wrong type": []byte("{}\n"),
	} {
		t.Run(name, func(t *testing.T) {
			command := exec.CommandContext(t.Context(), python, "-I", "-B", "-c", ProgramBootstrap)
			command.Dir, command.Env = filepath.Dir(python), []string{}
			command.Stdin, command.Stderr = bytes.NewReader(input), io.Discard
			output, err := command.Output()
			if err == nil || len(output) != 0 {
				t.Fatalf("invalid program record executed: %q, error = %v", output, err)
			}
		})
	}
}

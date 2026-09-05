package pythonfacts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const maximumProgramRecordSize = 1 << 20

const ProgramBootstrap = "import json,sys;sys.stdin.reconfigure(encoding='utf-8');sys.stdout.reconfigure(encoding='utf-8');_program=sys.stdin.buffer.readline(1048577);(0<len(_program)<=1048576 and _program.endswith(b'\\n')) or sys.exit('invalid policy program record');exec(compile(json.loads(_program),'<code-polishy>','exec'))"

func ProgramInput(source string, input io.Reader) (io.Reader, error) {
	if source == "" || len(source) > maximumProgramRecordSize || !utf8.ValidString(source) || strings.ContainsRune(source, 0) {
		return nil, fmt.Errorf("python policy program is empty, invalid, or oversized")
	}
	encoded, _ := json.Marshal(source)
	if len(encoded)+1 > maximumProgramRecordSize {
		return nil, fmt.Errorf("python policy program record exceeds %d bytes", maximumProgramRecordSize)
	}
	return io.MultiReader(bytes.NewReader(append(encoded, '\n')), input), nil
}

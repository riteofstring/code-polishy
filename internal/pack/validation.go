package pack

import "fmt"

type constraintError struct {
	path       string
	constraint string
}

func (problem constraintError) Error() string {
	return fmt.Sprintf("%s: expected %s", problem.path, problem.constraint)
}

func expected(path, constraint string) error {
	return constraintError{path: path, constraint: constraint}
}

func indexed(path string, index int) string {
	return fmt.Sprintf("%s[%d]", path, index)
}

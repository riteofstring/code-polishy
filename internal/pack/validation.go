package pack

import "fmt"

func expected(path, constraint string) error {
	return fmt.Errorf("%s: expected %s", path, constraint)
}

func indexed(path string, index int) string {
	return fmt.Sprintf("%s[%d]", path, index)
}

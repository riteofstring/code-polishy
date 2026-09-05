package engine

import "fmt"

func (engine *Engine) currentPythonReachabilityState(expected string) error {
	current, err := engine.Repository.PythonReachabilityStateSHA256()
	if err != nil {
		return err
	}
	if current != expected {
		return fmt.Errorf("python reachability dependency evidence changed during gate execution")
	}
	return nil
}

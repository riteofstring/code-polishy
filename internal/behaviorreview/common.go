package behaviorreview

import "context"

func allValid(values ...bool) bool {
	for _, value := range values {
		if !value {
			return false
		}
	}
	return true
}

func reviewContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

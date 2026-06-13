package flow

// Must panics if err is not nil, otherwise returns value.
func Must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}

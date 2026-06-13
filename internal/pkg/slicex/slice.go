package slicex

// Any checks if at least one element in the slice passes the test implemented by the provided function.
func Any[E any](elements []E, fn func(el E) bool) bool {
	for _, el := range elements {
		if fn(el) {
			return true
		}
	}
	return false
}

// All checks if all elements in the slice pass the test implemented by the provided function.
func All[E any](elements []E, fn func(el E) bool) bool {
	for _, el := range elements {
		if !fn(el) {
			return false
		}
	}
	return true
}

// Merge merges an arbitrary number of slices into one output slice.
func Merge[E any](inputs ...[]E) (output []E) {
	totalSize := 0
	for _, inp := range inputs {
		totalSize += len(inp)
	}
	output = make([]E, 0, totalSize)
	for _, inp := range inputs {
		output = append(output, inp...)
	}
	return
}

// Transform creates a new slice of type R populated with elements produced by fn.
func Transform[E, R any](elements []E, fn func(e E) R) []R {
	ns := make([]R, len(elements))
	for i := range elements {
		ns[i] = fn(elements[i])
	}
	return ns
}

// Filter returns a copy of the slice containing only elements that pass fn.
func Filter[E any](elements []E, fn func(e E) bool) []E {
	filtered := make([]E, 0)
	for _, e := range elements {
		if fn(e) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// First returns the first element of the slice, or the zero value if empty.
func First[E any](elements []E) E {
	return Get(elements, 0)
}

// Get returns the i-th element of the slice or the zero value if out of bounds.
func Get[E any](elements []E, i int) E {
	var el E
	if i >= 0 && len(elements) > i {
		return elements[i]
	}
	return el
}

// GroupBy groups elements by the key returned by fn.
func GroupBy[E any, G comparable](elements []E, fn func(item E) G) map[G][]E {
	result := map[G][]E{}
	for _, e := range elements {
		key := fn(e)
		result[key] = append(result[key], e)
	}
	return result
}

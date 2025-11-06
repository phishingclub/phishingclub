// Package ref provides a utility function for creating a pointer to a value.
// It is designed to simplify the process of obtaining a pointer to a value of any type.
package ref

// Of creates a pointer to the provided value of type 'E'.
// The primary purpose of this function is to simplify the creation of pointers to values
// without needing to use temporary variables.
//
// Parameters:
//
//	e: The value of type 'E' to create a pointer for.
//
// Returns:
//
//	*E: A pointer to the provided value 'e'.
//
// Example usage:
//
//	intValue := 42
//	intPtr := ref.Of(intValue)
//	fmt.Println(*intPtr)
func Of[E any](e E) *E { return &e }

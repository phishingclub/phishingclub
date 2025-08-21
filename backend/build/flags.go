package build

type flags struct {
	Production bool
}

// Flags is a global variable for build flags
var Flags = flags{
	Production: false,
}

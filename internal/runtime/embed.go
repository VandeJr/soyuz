package runtime

import _ "embed"

//go:embed src/rc.c
var Source []byte

//go:embed src/std_io.c
var StdIOSource []byte

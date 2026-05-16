package runtime

import _ "embed"

//go:embed src/soyuz.h
var SoyuzHeader []byte

//go:embed src/rc.c
var Source []byte

//go:embed src/std_io.c
var StdIOSource []byte

//go:embed src/std_string.c
var StdStringSource []byte

//go:embed src/std_fs.c
var StdFSSource []byte

//go:embed src/std_os.c
var StdOSSource []byte

//go:embed src/std_collections.c
var StdCollectionsSource []byte

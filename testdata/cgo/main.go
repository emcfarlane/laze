package main

// #include <stdio.h>
// #include <stdlib.h>
import "C"
import "unsafe"

func Print(s string) {
	cs := C.CString(s)
	C.fputs(cs, (*C.FILE)(C.stdout))
	C.free(unsafe.Pointer(cs))
}

func main() {
	Print("Hello, cgo!")
}

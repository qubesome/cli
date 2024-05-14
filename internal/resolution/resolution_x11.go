//go:build x11

package resolution

// Depends on libx11-devel.

/*
#cgo LDFLAGS: -lX11

#include <X11/Xlib.h>
*/
import "C"
import "fmt"

// Primary returns the screen resolution for the primary display.
func Primary() (string, error) {
	d := C.XOpenDisplay(nil)
	if d == nil {
		return "", fmt.Errorf("cannot open display")
	}
	defer C.XCloseDisplay(d)

	screen := C.XDefaultScreenOfDisplay(d)
	width := int(C.XWidthOfScreen(screen))
	if width <= 0 {
		return "", fmt.Errorf("cannot convert display width")
	}
	height := int(C.XHeightOfScreen(screen))
	if height <= 0 {
		return "", fmt.Errorf("cannot convert display height")
	}

	return fmt.Sprintf("%dx%d", width, height), nil
}

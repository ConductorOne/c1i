package cmd

import (
	"reflect"
	"testing"
)

// stdoutFields scans every cmd file; the closure walks call it per root.
func TestStdoutFieldsIsComputedOnce(t *testing.T) {
	ix := loadSrcIndex(t, nil)
	a, b := ix.stdoutFields(), ix.stdoutFields()
	if reflect.ValueOf(a).Pointer() != reflect.ValueOf(b).Pointer() {
		t.Fatal("stdoutFields rebuilt its result on the second call")
	}
}

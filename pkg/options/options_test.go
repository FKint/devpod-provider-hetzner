package options

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiskSizeGB(t *testing.T) {
	for in, want := range map[string]int{"10": 10, "50": 50} {
		got, err := (&Options{DiskSize: in}).DiskSizeGB()
		assert.NoError(t, err)
		assert.Equal(t, want, got)
	}

	for _, in := range []string{"", "abc", "9", "-1"} {
		_, err := (&Options{DiskSize: in}).DiskSizeGB()
		assert.Error(t, err, in)
	}
}

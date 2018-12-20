package testproject

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestSumpthin(t *testing.T) {

	fmt.Print("Running nonsense test.")

	assert.True(t, 1 == 1, "Basic law of Identity")
}

// +build acctest
// NOTE: We use build tags to differentiate acceptance testing

package test

import (
	"testing"

	"github.com/aristosvo/aztfmove/acceptance"
	"github.com/arschles/assert"
	"github.com/gruntwork-io/terratest/modules/terraform"
)

func TestStorage_Basic(t *testing.T) {
	t.Parallel()

	terraformOptions := &terraform.Options{
		TerraformDir: "./",
	}
	defer terraform.Destroy(t, terraformOptions)
	terraform.InitAndApply(t, terraformOptions)

	moveStorage := []string{"-resource-group=input-sa-rg", "-target-resource-group=output-sa-rg"}
	acceptance.Step(moveStorage, t)

	moveStorageBack := []string{"-target-resource-group=input-sa-rg"}
	acceptance.Step(moveStorageBack, t)

	exitCode := terraform.InitAndPlanWithExitCode(t, terraformOptions)
	assert.Equal(t, exitCode, 0, "terraform plan exitcode")
}

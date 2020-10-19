# Projectwork Checking Automation

This little tool automates the checking of project work submissions using [Docker](https://docker.io) and [Cucumber](https://cucumber.io/). It takes the feature files described in the [features](./features) directory and translates them to Go functions, some of which execute in a Docker container.

## How Cucumber is used

Cucumber is integrated using [GoDog](https://github.com/cucumber/godog), a test framework for Go executing feature files. [main.go](main.go) sets up the test environment for each subdirectory in a specified directory and executes the steps.

The steps are defined in [steps.go](steps.go):

```go
func (tc *TestContext) InitializeScenario(ctx *godog.ScenarioContext) {
	ctx.Step(`^I have applied the Terraform code$`, tc.iHaveAppliedTheTerraformCode)
    ...
}
```

Each sentence is matched with a function, which is called when that sentence is used in a feature file. Some functions can have parameters that are automatically extracted based on the regular expression.

### How Docker is integrated

Terraform is executed in a Docker container for security purposes. This is done in the function `executeTerraform`. It 
will connect the Docker socket and launch a new container for each Terraform execution.
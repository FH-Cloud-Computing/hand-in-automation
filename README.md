# Projectwork Checking Automation

This little tool automates the checking of project work submissions using [Docker](https://docker.io) and [Cucumber](https://cucumber.io/). It takes the feature files described in the [features](./features) directory and translates them to Go functions, some of which execute in a Docker container.

## Getting it to run

1. Install the Docker Desktop.
2. Download the appropriate release from the [releases section](https://github.com/FH-Cloud-Computing/hand-in-automation/releases/).
3. Unpack all files to a directory.
4. Set the `EXOSCALE_KEY`, `EXOSCALE_SECRET` environment variables.
5. Set the `DIRECTORY` variable to point to the directory with your source code.
6. Run the `hand-in-automation` program from its directory. 

## How it works

### How Cucumber is used

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
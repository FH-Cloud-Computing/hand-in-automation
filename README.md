# Projectwork Checking Automation

This little tool automates the checking of project work submissions using [Docker](https://docker.io) and [Cucumber](https://cucumber.io/). It takes the feature files described in the [features](./features) directory and translates them to Go functions, some of which execute in a Docker container.

**Important:** running this utility doesn't replace testing your code yourself! If bugs or missing checks are found they may be added last minute!

## Getting it to run

1. Install the Docker Desktop.
2. Download the appropriate release from the [releases section](https://github.com/FH-Cloud-Computing/hand-in-automation/releases/).
3. Unpack all files to a directory.
4. Set the `EXOSCALE_KEY`, `EXOSCALE_SECRET` environment variables.
5. Set the `DIRECTORY` variable to point to the directory with your source code.
6. Run the `hand-in-automation` program from its directory, optionally with the `-v` or `-vv` parameters to get a more detailed output.

**Note:** if you do not wish to check for the optional service discovery goal please remove or rename the `sprint2-servicediscovery.feature` file from the `features` directory.

## Getting more info & making runs faster

This tool has a command-line switch to enable more verbose logging. It can be triggered by passing the `-v` option, or the `-vv` option for increasingly verbose logs.

If you want to run only some of the tests you can remove or rename the feature files in the release.

## How it works

### How Cucumber is used

Cucumber is integrated using [GoDog](https://github.com/cucumber/godog), a test framework for Go executing feature files. [main.go](main.go) sets up the test environment and executes the steps.

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

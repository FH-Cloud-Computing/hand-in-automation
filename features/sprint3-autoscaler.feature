Feature: Sprint 3 Autoscaler
  To achieve this goal, you must implement the autoscaler yourself. (Do not copy code from the Internet or from fellow
  students.) You can pick the programming language of your choice.

  Scenario: Autoscaler scaling up [optional]
    Given I build the Dockerfile in the "autoscaler" folder
    And I apply the Terraform code
    And I start a container from the image
    And I set the instance pool to have 1 instances
    And I kill all instances in the pool
    And I wait for 1 instances to be present
    And all backends should be healthy after 900 seconds
    When I send a webhook the "/up" endpoint of the container
    Then there should be 2 instances present after 900 seconds
    And I send a webhook the "/down" endpoint of the container
    And there should be 1 instances present after 900 seconds
    And I send a webhook the "/down" endpoint of the container
    And there should be 1 instances present after 900 seconds
    And I should be able to stop the container with a 0 exit code

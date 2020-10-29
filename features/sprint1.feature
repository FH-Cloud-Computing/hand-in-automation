Feature: Sprint 1
  Your webservice must answer on an IP address and balance traffic across all instances running in an instance pool.
  The number of instances will be changed in the demonstration and your web service must adapt accordingly.

  Scenario: Creating resources
    When I apply the Terraform code

  Scenario: Building blocks
    When I apply the Terraform code
    Then one instance pool should exist
    And one NLB should exist
    And the NLB should have one service
    And the service should listen to port 80
    And all backends should be healthy after 600 seconds
    And I should receive the answer "OK" when querying the "/health" endpoint of the NLB

  Scenario: Scaling up the instance pool
    When I apply the Terraform code
    And I set the instance pool to have 2 instances
    And I kill all instances in the pool
    And I wait for 2 instances to be present
    Then all backends should be healthy after 600 seconds
    And I should receive the answer "OK" when querying the "/health" endpoint of the NLB

  Scenario: Removing resources
    When I apply the Terraform code
    And I destroy using the Terraform code
    Then the tfstate file should be empty

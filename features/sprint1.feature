Feature: Sprint 1
  Your webservice must answer on an IP address and balance traffic across all instances running in an instance pool.
  The number of instances will be changed in the demonstration and your web service must adapt accordingly.

  Scenario: Sprint 1: Creating resources
    When I apply the Terraform code
    Then one instance pool should exist
    And one NLB should exist
    And the NLB should have one service
    And the service should listen to port 80
    And all backends should be healthy after 900 seconds
    And I should receive the answer "OK" when querying the "/health" endpoint of the NLB

  Scenario: Sprint 1: Scaling up the instance pool
    When I apply the Terraform code
    And I set the instance pool to have 1 instances
    And I kill all instances in the pool
    And I wait for 1 instances to be present
    Then all backends should be healthy after 900 seconds
    And I should receive the answer "OK" when querying the "/health" endpoint of the NLB

  Scenario: Sprint 1: Removing resources
    When I apply the Terraform code
    And I destroy using the Terraform code
    Then the tfstate file should be empty

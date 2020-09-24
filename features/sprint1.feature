Feature: Sprint 1
  Your webservice must answer on an IP address and balance traffic across all instances running in an instance pool.
  The number of instances will be changed in the demonstration and your web service must adapt accordingly.

  Background:
    Given I have applied the Terraform code

  Scenario: The instance pool should exist
    Then one instance pool should exist

  Scenario: An NLB should exist
    Then one NLB should exist

  Scenario: The NLB should have one service
    Then the NLB should have one service
    And the service should listen to port 80
    And all backends should be healthy after 300 seconds

  Scenario: The service should respond
    Then I should receive the answer "OK" when querying the "/health" endpoint of the NLB

  Scenario: Scaling up the instance pool
    When I resize the instance pool to zero
    And I wait for no instances to be present
    And I resize the instance pool to two
    And I wait for 2 instances to be present
    Then I should receive the answer "OK" when querying the "/health" endpoint of the NLB

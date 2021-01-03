Feature: Sprint 3
  In this sprint you must demonstrate your ability to receive a webhook from Grafana and adjust the number of instances
  in the instance pool from the previous sprint under load.

  Scenario: Adding load to the NLB
    Given I apply the Terraform code
    And I set the instance pool to have 1 instances
    And I kill all instances in the pool
    And I wait for 1 instances to be present
    And all backends are healthy after 900 seconds
    When I start hitting the NLB on 10 parallel threads
    Then there should be 2 instances present after 300 seconds

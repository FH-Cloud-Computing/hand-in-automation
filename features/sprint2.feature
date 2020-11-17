Feature: Sprint 2
  In this sprint you must demonstrate your ability to monitor a varying number of instances set up on an instance pool
  in the previous sprint using Prometheus.

  Scenario: Sprint 2: Building blocks
    When I apply the Terraform code
    Then there must be a monitoring server
    And Prometheus must be accessible on port 9090 of the monitoring server within 900 seconds
    And CPU metrics of all instances in the instance pool must be visible in Prometheus on port 9090 after 900 seconds

  Scenario: Sprint 2: Killing instances
    When I apply the Terraform code
    And I set the instance pool to have 1 instances
    Then all backends should be healthy after 900 seconds
    And CPU metrics of all instances in the instance pool must be visible in Prometheus on port 9090 after 900 seconds


Feature: Sprint 2 Service Discovery
  To achieve this goal, you must implement the service discovery agent yourself. (Do not copy code from the Internet.)
  You can pick the programming language of your choice.

  Scenario: Testing the service discovery
    When I build the Dockerfile in the "servicediscovery" folder
    And I apply the Terraform code
    And I start a container from the image
    And I set the instance pool to have 2 instances
    Then all backends should be healthy after 600 seconds
    And the service discovery file must contain all instance pool IPs within 120 seconds

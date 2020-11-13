Feature: Sprint 2 Service Discovery
  To achieve this goal, you must implement the service discovery agent yourself. (Do not copy code from the Internet.)
  You can pick the programming language of your choice.

  # In this scenario we are applying the Terraform code to create an instance pool, then launching
  # the service discovery agent. We then check if all IPs from the instance pool are present in the
  # service discovery file with the correct randomized port numbers. The port numbers are randomized
  # so that the service discovery agent is truly generic and can deal with various exporters running
  # on differnt ports, not just the Prometheus node exporter. Finally, we check if the service
  # discovery agent shuts down properly.
  Scenario: Testing the service discovery
    When I build the Dockerfile in the "servicediscovery" folder
    And I apply the Terraform code
    And I start a container from the image
    And I set the instance pool to have 2 instances
    And I kill all instances in the pool
    And I wait for 2 instances to be present
    Then all backends should be healthy after 600 seconds
    And the service discovery file must contain all instance pool IPs within 120 seconds
    And I should be able to stop the container with a 0 exit code

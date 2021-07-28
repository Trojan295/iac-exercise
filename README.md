# IaC exercise

This repository shows an example program to upgrade EC2 instances behind an ELB. What it does:

1. Detect all EC2 instances with the given AMI
2. Find the ELB for the instances
3. Deploy new EC2 instances with the same configuration, except with a new AMI
4. Attach the new EC2 instances to the ELB
5. Wait till the new EC2s are serving traffic
6. Terminate the old EC2s

## Prerequisites

- AWS CLI v2
- Golang compiler 1.16

## Compile the `deploy` program

The program to deploy the new EC2 instances is written in Go, thus it requires to be compiled.

To compile the program, run:
```bash
make build
```

You should now have the `deploy` binary in the current directory.

## Testing

To run the tests:
```bash
make test
```

## How to run it

1. Deploy the environment using CloudFormation:
   ```
   make bootstrap-env
   ```

   The script will print the DNS of the LoadBalancer at the end.
   

2. Verify the environment is working. This can take a few minutes, as the domain name needs to propagate. Access the LoadBalancer DNS to check, if the webpage is working:

   ```bash
   curl http://<your-loadbalancer-dns>
   ```
 
   You should see the deployment date and the AMI ID of the instance on the webpage.

3. Execute the `deploy` binary to perform a deployment of new EC2 instances.

   ```bash
   ./deploy <old-ami-id> <new-ami-id>
   ```

   It will create new EC2 instances, attach them to the load balancer, then detach and remove the old EC2 instances.

4. Verify again, if the webpage is working. You should the new AMI ID on the webpage.

## Cleanup

To remove all resources you have to:

1. Remove the EC2 instances manually. They cannot be removed using CloudFormation, because they were created using the `deploy` program.
2. Delete the CloudFormation stack:
    ```bash
    make destroy-env
    ```
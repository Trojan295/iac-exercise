package pkg

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/avast/retry-go"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elb "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing/types"
)

type DeployInput struct {
	OldAmiID string
	NewAmiID string
}

type EC2Client interface {
	DescribeInstances(ctx context.Context, params *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	RunInstances(ctx context.Context, params *ec2.RunInstancesInput, optFns ...func(*ec2.Options)) (*ec2.RunInstancesOutput, error)
	TerminateInstances(ctx context.Context, params *ec2.TerminateInstancesInput, optFns ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error)
}

type ELBClient interface {
	DescribeLoadBalancers(ctx context.Context, params *elb.DescribeLoadBalancersInput, optFns ...func(*elb.Options)) (*elb.DescribeLoadBalancersOutput, error)
	RegisterInstancesWithLoadBalancer(ctx context.Context, params *elb.RegisterInstancesWithLoadBalancerInput, optFns ...func(*elb.Options)) (*elb.RegisterInstancesWithLoadBalancerOutput, error)
	DescribeInstanceHealth(ctx context.Context, params *elb.DescribeInstanceHealthInput, optFns ...func(*elb.Options)) (*elb.DescribeInstanceHealthOutput, error)
}

type Deployer struct {
	ec2Client EC2Client
	elbClient ELBClient
	userData  *string
}

func NewDeployer(ec2Client EC2Client, elbClient ELBClient, userData *string) *Deployer {
	return &Deployer{
		ec2Client: ec2Client,
		elbClient: elbClient,
		userData:  userData,
	}
}

func (d *Deployer) Deploy(ctx context.Context, input *DeployInput) error {
	oldInstances, err := d.getOldInstances(ctx, input.OldAmiID)
	if err != nil {
		return fmt.Errorf("while getting old instances: %v", err)
	}

	log.Printf("Found %d instances to replace", len(oldInstances))

	elb, err := d.findLoadBalancer(ctx, oldInstances)
	if err != nil {
		// TODO: rollback
		return fmt.Errorf("while finding ELB: %v", err)
	}

	log.Printf("Found ELB %s to serve traffic", *elb.LoadBalancerName)

	newInstances, err := d.createNewInstances(ctx, oldInstances, input.NewAmiID)
	if err != nil {
		// TODO: rollback
		return fmt.Errorf("while creating new instances: %v", err)
	}

	log.Printf("Created %d new instances", len(newInstances))

	err = d.attachInstancesToLoadBalancer(ctx, elb, newInstances)
	if err != nil {
		// TODO: rollback
		return fmt.Errorf("while attaching instances to load balancer: %v", err)
	}

	log.Printf("Attached new instances to the ELB")
	log.Printf("Waiting for new instances to be InService. This can take a few minutes...")

	err = d.ensureInstancesAreInService(ctx, elb, newInstances)
	if err != nil {
		// TODO: rollback
		return fmt.Errorf("while ensuring new instances are InService: %v", err)
	}

	log.Printf("New instances are InService")

	err = d.terminateTypeInstances(ctx, oldInstances)
	if err != nil {
		return fmt.Errorf("while terminating old instances: %v", err)
	}

	log.Printf("Terminated old instances")

	return nil
}

func (d *Deployer) getOldInstances(ctx context.Context, oldAmiID string) ([]ec2types.Instance, error) {
	out, err := d.ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("image-id"),
				Values: []string{oldAmiID},
			},
			{
				Name:   aws.String("instance-state-name"),
				Values: []string{"running"},
			},
		},
	})

	if err != nil {
		return nil, fmt.Errorf("while describing instances: %v", err)
	}

	instances := make([]ec2types.Instance, 0)
	for _, reservation := range out.Reservations {
		instances = append(instances, reservation.Instances...)
	}

	return instances, nil
}

func (d *Deployer) createNewInstances(ctx context.Context, oldInstances []ec2types.Instance, newAmiID string) ([]ec2types.Instance, error) {
	newInstances := make([]ec2types.Instance, 0, len(oldInstances))

	for _, instance := range oldInstances {
		newInstance, err := d.createNewInstance(ctx, &instance, newAmiID)
		if err != nil {
			return newInstances, fmt.Errorf("while creating new instance: %v", err)
		}

		newInstances = append(newInstances, *newInstance)
	}

	return newInstances, nil
}

func (d *Deployer) createNewInstance(ctx context.Context, oldInstance *ec2types.Instance, newAmiID string) (*ec2types.Instance, error) {
	securityGroupIDs := make([]string, 0, len(oldInstance.SecurityGroups))
	for _, sg := range oldInstance.SecurityGroups {
		securityGroupIDs = append(securityGroupIDs, *sg.GroupId)
	}

	tags := make([]ec2types.Tag, 0)
	for _, tag := range oldInstance.Tags {
		if strings.HasPrefix(*tag.Key, "aws") {
			continue
		}

		tags = append(tags, tag)
	}

	out, err := d.ec2Client.RunInstances(ctx, &ec2.RunInstancesInput{
		MinCount:         aws.Int32(1),
		MaxCount:         aws.Int32(1),
		InstanceType:     oldInstance.InstanceType,
		ImageId:          &newAmiID,
		SubnetId:         oldInstance.SubnetId,
		SecurityGroupIds: securityGroupIDs,
		UserData:         d.userData,
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeInstance,
				Tags:         tags,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("while running new instance: %v", err)
	}

	return &out.Instances[0], nil
}

func (d *Deployer) findLoadBalancer(ctx context.Context, instances []ec2types.Instance) (*elbtypes.LoadBalancerDescription, error) {
	out, err := d.elbClient.DescribeLoadBalancers(ctx, &elb.DescribeLoadBalancersInput{})
	if err != nil {
		return nil, fmt.Errorf("while describing ELBs: %v", err)
	}

	for _, elb := range out.LoadBalancerDescriptions {
		if *elb.VPCId == *instances[0].VpcId {
			return &elb, nil
		}
	}

	return nil, ErrELBNotFound
}

func (d *Deployer) attachInstancesToLoadBalancer(ctx context.Context, loadBalancer *elbtypes.LoadBalancerDescription, instances []ec2types.Instance) error {
	elbInstances := make([]elbtypes.Instance, 0, len(instances))
	for _, inst := range instances {
		elbInstances = append(elbInstances, elbtypes.Instance{
			InstanceId: inst.InstanceId,
		})
	}

	_, err := d.elbClient.RegisterInstancesWithLoadBalancer(ctx, &elb.RegisterInstancesWithLoadBalancerInput{
		LoadBalancerName: loadBalancer.LoadBalancerName,
		Instances:        elbInstances,
	})
	if err != nil {
		return fmt.Errorf("while registering instances with ELB: %v", err)
	}

	return nil
}

func (d *Deployer) ensureInstancesAreInService(ctx context.Context, loadBalancer *elbtypes.LoadBalancerDescription, instances []ec2types.Instance) error {
	err := retry.Do(func() error {
		healthy, err := d.checkInstancesHealth(ctx, loadBalancer, instances)
		if err != nil {
			return err
		}

		if !healthy {
			return ErrInstancesNotInService
		}

		return nil
	},
		retry.Context(ctx),
		retry.Delay(10*time.Second),
	)

	return err
}

func (d *Deployer) checkInstancesHealth(ctx context.Context, loadBalancer *elbtypes.LoadBalancerDescription, instances []ec2types.Instance) (bool, error) {
	elbInstances := make([]elbtypes.Instance, 0, len(instances))
	for _, inst := range instances {
		elbInstances = append(elbInstances, elbtypes.Instance{
			InstanceId: inst.InstanceId,
		})
	}

	out, err := d.elbClient.DescribeInstanceHealth(ctx, &elb.DescribeInstanceHealthInput{
		LoadBalancerName: loadBalancer.LoadBalancerName,
		Instances:        elbInstances,
	})
	if err != nil {
		return false, fmt.Errorf("while describing instances health on ELB: %v", err)
	}

	for _, state := range out.InstanceStates {
		if *state.State != "InService" {
			return false, nil
		}
	}

	return true, nil
}

func (d *Deployer) terminateTypeInstances(ctx context.Context, instances []types.Instance) error {
	IDs := make([]string, 0, len(instances))
	for _, inst := range instances {
		IDs = append(IDs, *inst.InstanceId)
	}

	_, err := d.ec2Client.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: IDs,
	})
	if err != nil {
		return fmt.Errorf("while terminating instances: %v", err)
	}

	return nil
}

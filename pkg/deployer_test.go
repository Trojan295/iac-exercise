package pkg_test

import (
	"context"
	"testing"

	"github.com/Trojan295/iac-exercise/pkg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elb "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type ec2ClientMock struct {
	mock.Mock
}

func (m *ec2ClientMock) DescribeInstances(ctx context.Context, params *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	args := m.Called(ctx, params, optFns)
	return args.Get(0).(*ec2.DescribeInstancesOutput), args.Error(1)
}

func (m *ec2ClientMock) RunInstances(ctx context.Context, params *ec2.RunInstancesInput, optFns ...func(*ec2.Options)) (*ec2.RunInstancesOutput, error) {
	args := m.Called(ctx, params, optFns)
	return args.Get(0).(*ec2.RunInstancesOutput), args.Error(1)
}

func (m *ec2ClientMock) TerminateInstances(ctx context.Context, params *ec2.TerminateInstancesInput, optFns ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error) {
	args := m.Called(ctx, params, optFns)
	return args.Get(0).(*ec2.TerminateInstancesOutput), args.Error(1)
}

type elbClientMock struct {
	mock.Mock
}

func (m *elbClientMock) DescribeLoadBalancers(ctx context.Context, params *elb.DescribeLoadBalancersInput, optFns ...func(*elb.Options)) (*elb.DescribeLoadBalancersOutput, error) {
	args := m.Called(ctx, params, optFns)
	return args.Get(0).(*elb.DescribeLoadBalancersOutput), args.Error(1)
}

func (m *elbClientMock) RegisterInstancesWithLoadBalancer(ctx context.Context, params *elb.RegisterInstancesWithLoadBalancerInput, optFns ...func(*elb.Options)) (*elb.RegisterInstancesWithLoadBalancerOutput, error) {
	args := m.Called(ctx, params, optFns)
	return args.Get(0).(*elb.RegisterInstancesWithLoadBalancerOutput), args.Error(1)
}

func (m *elbClientMock) DescribeInstanceHealth(ctx context.Context, params *elb.DescribeInstanceHealthInput, optFns ...func(*elb.Options)) (*elb.DescribeInstanceHealthOutput, error) {
	args := m.Called(ctx, params, optFns)
	return args.Get(0).(*elb.DescribeInstanceHealthOutput), args.Error(1)
}

var noEC2Opts []func(*ec2.Options) = nil
var noELBOpts []func(*elb.Options) = nil

func TestDeploymentHappyPath(t *testing.T) {
	// Setup
	ctx := context.Background()

	oldAmiID := "ami-123"
	newAmiID := "ami-987"

	vpcId := "vpc-1"
	subnets := []string{"subnet-1", "subnet-2", "subnet-3"}

	ec2SecurityGroup := "sg-1"

	oldInstanceIDs := []string{"i-1", "i-2", "i-3"}
	newInstanceIDs := []string{"i-1-new", "i-2-new", "i-3-new"}

	correctElb := getLoadBalancerDescription("test-elb", vpcId)
	otherElb := getLoadBalancerDescription("wrong-elb", "other-vpc")

	currentTypeInstances := []ec2types.Instance{}
	for i, ID := range oldInstanceIDs {
		currentTypeInstances = append(
			currentTypeInstances,
			getTypeInstanceStub(ID, oldAmiID, vpcId, subnets[i], []string{ec2SecurityGroup}),
		)
	}

	ec2Client := &ec2ClientMock{}
	elbClient := &elbClientMock{}

	ec2Client.
		On("DescribeInstances", ctx, &ec2.DescribeInstancesInput{
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
		}, noEC2Opts).
		Return(&ec2.DescribeInstancesOutput{
			Reservations: []ec2types.Reservation{
				{
					Instances: currentTypeInstances,
				},
			},
		}, nil)

	elbClient.
		On("DescribeLoadBalancers", ctx, &elb.DescribeLoadBalancersInput{}, noELBOpts).
		Return(&elb.DescribeLoadBalancersOutput{
			LoadBalancerDescriptions: []elbtypes.LoadBalancerDescription{
				correctElb,
				otherElb,
			},
		}, nil)

	for i, instance := range currentTypeInstances {
		ec2Client.
			On("RunInstances", ctx, &ec2.RunInstancesInput{
				MinCount:         aws.Int32(1),
				MaxCount:         aws.Int32(1),
				InstanceType:     instance.InstanceType,
				ImageId:          &newAmiID,
				SubnetId:         instance.SubnetId,
				SecurityGroupIds: []string{ec2SecurityGroup},
				UserData:         nil,
				TagSpecifications: []ec2types.TagSpecification{
					{
						ResourceType: ec2types.ResourceTypeInstance,
						Tags:         instance.Tags,
					},
				},
			}, noEC2Opts).
			Return(&ec2.RunInstancesOutput{
				Instances: []ec2types.Instance{
					{
						InstanceId: aws.String(newInstanceIDs[i]),
					},
				},
			}, nil)
	}

	elbClient.
		On("RegisterInstancesWithLoadBalancer", ctx, &elb.RegisterInstancesWithLoadBalancerInput{
			LoadBalancerName: correctElb.LoadBalancerName,
			Instances:        getElbInstances(newInstanceIDs),
		}, noELBOpts).
		Return(&elb.RegisterInstancesWithLoadBalancerOutput{
			Instances: getElbInstances(newInstanceIDs),
		}, nil)

	newInstanceStates := []elbtypes.InstanceState{}
	for _, ID := range newInstanceIDs {
		newInstanceStates = append(newInstanceStates, elbtypes.InstanceState{
			InstanceId: &ID,
			State:      aws.String("InService"),
		})
	}

	elbClient.
		On("DescribeInstanceHealth", ctx, &elb.DescribeInstanceHealthInput{
			LoadBalancerName: correctElb.LoadBalancerName,
			Instances:        getElbInstances(newInstanceIDs),
		}, noELBOpts).
		Return(&elb.DescribeInstanceHealthOutput{
			InstanceStates: newInstanceStates,
		}, nil)

	ec2Client.
		On("TerminateInstances", ctx, &ec2.TerminateInstancesInput{
			InstanceIds: oldInstanceIDs,
		}, noEC2Opts).
		Return(&ec2.TerminateInstancesOutput{}, nil)

	// Test
	deployer := pkg.NewDeployer(ec2Client, elbClient, nil)

	err := deployer.Deploy(ctx, &pkg.DeployInput{
		OldAmiID: oldAmiID,
		NewAmiID: newAmiID,
	})

	// Assert
	assert.Nil(t, err)
	ec2Client.AssertExpectations(t)
	elbClient.AssertExpectations(t)
}

func getTypeInstanceStub(id, ami, vpcId, subnetId string, sgIds []string) ec2types.Instance {
	sgs := make([]ec2types.GroupIdentifier, 0, len(sgIds))
	for _, id := range sgIds {
		sgs = append(sgs, ec2types.GroupIdentifier{GroupId: aws.String(id)})
	}

	return ec2types.Instance{
		InstanceId:     aws.String(id),
		ImageId:        aws.String(ami),
		VpcId:          aws.String(vpcId),
		SubnetId:       aws.String(subnetId),
		SecurityGroups: sgs,
		Tags: []ec2types.Tag{
			{
				Key:   aws.String("Name"),
				Value: aws.String(id),
			},
		},
	}
}

func getLoadBalancerDescription(name string, vpcID string) elbtypes.LoadBalancerDescription {
	return elbtypes.LoadBalancerDescription{
		LoadBalancerName: aws.String(name),
		VPCId:            aws.String(vpcID),
	}
}

func getElbInstances(IDs []string) []elbtypes.Instance {
	instances := make([]elbtypes.Instance, 0, len(IDs))
	for _, id := range IDs {
		instances = append(instances, elbtypes.Instance{
			InstanceId: aws.String(id),
		})
	}

	return instances
}

package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

var ErrEC2CredentialsMissing = errors.New("AWS credentials are not configured")

type EC2InstanceState struct {
	InstanceID string
	State      string
	PublicIP   string
}

type EC2InstanceClient interface {
	StopInstance(ctx context.Context, region, instanceID string) error
	StartInstance(ctx context.Context, region, instanceID string) error
	WaitForInstanceState(ctx context.Context, region, instanceID, desiredState string, timeout time.Duration) (*EC2InstanceState, error)
	DescribeInstance(ctx context.Context, region, instanceID string) (*EC2InstanceState, error)
}

type AWSEC2InstanceClient struct {
	newClient func(ctx context.Context, region string) (*ec2.Client, error)
}

func NewAWSEC2InstanceClient() *AWSEC2InstanceClient {
	return &AWSEC2InstanceClient{
		newClient: loadEC2Client,
	}
}

func (c *AWSEC2InstanceClient) StopInstance(ctx context.Context, region, instanceID string) error {
	client, err := c.newClient(ctx, region)
	if err != nil {
		return err
	}

	_, err = client.StopInstances(ctx, &ec2.StopInstancesInput{
		InstanceIds: []string{instanceID},
	})
	if err != nil {
		return fmt.Errorf("stop ec2 instance %s: %w", instanceID, err)
	}
	return nil
}

func (c *AWSEC2InstanceClient) StartInstance(ctx context.Context, region, instanceID string) error {
	client, err := c.newClient(ctx, region)
	if err != nil {
		return err
	}

	_, err = client.StartInstances(ctx, &ec2.StartInstancesInput{
		InstanceIds: []string{instanceID},
	})
	if err != nil {
		return fmt.Errorf("start ec2 instance %s: %w", instanceID, err)
	}
	return nil
}

func (c *AWSEC2InstanceClient) WaitForInstanceState(ctx context.Context, region, instanceID, desiredState string, timeout time.Duration) (*EC2InstanceState, error) {
	deadline := time.Now().Add(timeout)
	for {
		state, err := c.DescribeInstance(ctx, region, instanceID)
		if err != nil {
			return nil, err
		}
		if strings.EqualFold(state.State, desiredState) {
			return state, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for instance %s to reach state %s (last state: %s)", instanceID, desiredState, state.State)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func (c *AWSEC2InstanceClient) DescribeInstance(ctx context.Context, region, instanceID string) (*EC2InstanceState, error) {
	client, err := c.newClient(ctx, region)
	if err != nil {
		return nil, err
	}

	output, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceID},
	})
	if err != nil {
		return nil, fmt.Errorf("describe ec2 instance %s: %w", instanceID, err)
	}

	for _, reservation := range output.Reservations {
		for _, instance := range reservation.Instances {
			if aws.ToString(instance.InstanceId) != instanceID {
				continue
			}
			return &EC2InstanceState{
				InstanceID: instanceID,
				State:      string(instance.State.Name),
				PublicIP:   aws.ToString(instance.PublicIpAddress),
			}, nil
		}
	}

	return nil, fmt.Errorf("ec2 instance %s not found", instanceID)
}

func loadEC2Client(ctx context.Context, region string) (*ec2.Client, error) {
	accessKey := strings.TrimSpace(os.Getenv("AWS_ACCESS_KEY_ID"))
	secretKey := strings.TrimSpace(os.Getenv("AWS_SECRET_ACCESS_KEY"))
	if accessKey == "" || secretKey == "" {
		return nil, ErrEC2CredentialsMissing
	}

	if strings.TrimSpace(region) == "" {
		region = strings.TrimSpace(os.Getenv("AWS_DEFAULT_REGION"))
	}
	if region == "" {
		region = "us-east-1"
	}

	sessionToken := strings.TrimSpace(os.Getenv("AWS_SESSION_TOKEN"))
	cfg, err := config.LoadDefaultConfig(
		ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, sessionToken)),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	return ec2.NewFromConfig(cfg), nil
}

func isEC2InstanceTerminated(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case string(ec2types.InstanceStateNameTerminated), string(ec2types.InstanceStateNameShuttingDown):
		return true
	default:
		return false
	}
}

package main

import (
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/eks"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Create service role for EKS
		eksRole, err := iam.NewRole(ctx, "eksClusterRole", &iam.RoleArgs{
			AssumeRolePolicy: pulumi.String(`{
				"Version": "2008-10-17",
				"Statement": [{
					"Sid": "",
					"Effect": "Allow",
					"Principal": {
						"Service": "eks.amazonaws.com"
					},
					"Action": "sts:AssumeRole"
				}]
			}`),
			ManagedPolicyArns: pulumi.StringArray{
				pulumi.String("arn:aws:iam::aws:policy/AmazonEKSClusterPolicy"),
			},
		})
		if err != nil {
			return err
		}

		// Get all subnets in the VPC, filter out those in us-east-1e, and pick 2 random subnet IDs
		t := true
		vpc, err := ec2.LookupVpc(ctx, &ec2.LookupVpcArgs{Default: &t})
		if err != nil {
			return err
		}
		subnets, err := ec2.GetSubnets(ctx, &ec2.GetSubnetsArgs{
			Filters: []ec2.GetSubnetsFilter{
				{Name: "vpc-id", Values: []string{vpc.Id}},
				{Name: "availability-zone", Values: []string{"us-east-1e"}},
			},
		})

		// Create EKS cluster
		cluster, err := eks.NewCluster(ctx, "demo-eks", &eks.ClusterArgs{
			RoleArn: pulumi.StringInput(eksRole.Arn),
			AccessConfig: &eks.ClusterAccessConfigArgs{
				AuthenticationMode: pulumi.String("API_AND_CONFIG_MAP"),
			},
		})
	})
}

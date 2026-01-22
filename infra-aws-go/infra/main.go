package main

import (
	"encoding/json"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/eks"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		tmpJSON0, err := json.Marshal(map[string]any{
			"Version": "2012-10-17",
			"Statement": []map[string]any{
				{
					"Action": []string{
						"sts:AssumeRole",
						"sts:TagSession",
					},
					"Effect": "Allow",
					"Principal": map[string]any{
						"Service": "eks.amazonaws.com",
					},
				},
			},
		})
		if err != nil {
			return err
		}

		json0 := string(tmpJSON0)
		cluster, err := iam.NewRole(ctx, "clusterRole", &iam.RoleArgs{
			Name:             pulumi.String("eksClusterRole"),
			AssumeRolePolicy: pulumi.String(json0),
		})
		if err != nil {
			return err
		}

		clusterAmazonEKSClusterPolicy, err := iam.NewRolePolicyAttachment(
			ctx,
			"cluster_AmazonEKSClusterPolicy",
			&iam.RolePolicyAttachmentArgs{
				PolicyArn: pulumi.String("arn:aws:iam::aws:policy/AmazonEKSClusterPolicy"),
				Role:      cluster.Name,
			},
		)
		if err != nil {
			return err
		}

		t := true
		vpc, err := ec2.LookupVpc(ctx, &ec2.LookupVpcArgs{
			Default: &t,
		})
		if err != nil {
			return err
		}

		subnetIds, err := ec2.GetSubnets(ctx, &ec2.GetSubnetsArgs{
			Filters: []ec2.GetSubnetsFilter{
				{
					Name:   "vpc-id",
					Values: []string{vpc.Id},
				},
				{
					Name: "availability-zone",
					Values: []string{
						"us-east-1a",
						"us-east-1b",
					},
				},
			},
		})
		if err != nil {
			return err
		}

		_, err = eks.NewCluster(ctx, "demoEks", &eks.ClusterArgs{
			Name: pulumi.String("demo-eks"),
			AccessConfig: &eks.ClusterAccessConfigArgs{
				AuthenticationMode: pulumi.String("API"),
			},
			RoleArn: cluster.Arn,
			Version: pulumi.String("1.33"),
			VpcConfig: &eks.ClusterVpcConfigArgs{
				SubnetIds: pulumi.ToStringArray(subnetIds.Ids),
			},
		}, pulumi.DependsOn([]pulumi.Resource{
			clusterAmazonEKSClusterPolicy,
		}))
		if err != nil {
			return err
		}
		return nil
	})
}

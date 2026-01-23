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
		cluster, err := iam.NewRole(ctx, "cluster-role", &iam.RoleArgs{
			Name:             pulumi.String("eksClusterRole"),
			AssumeRolePolicy: pulumi.String(json0),
		})
		if err != nil {
			return err
		}

		clusterAmazonEKSClusterPolicy, err := iam.NewRolePolicyAttachment(
			ctx,
			"cluster-AmazonEKSClusterPolicy",
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

		eksCluster, err := eks.NewCluster(ctx, "demo-eks", &eks.ClusterArgs{
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

		tmpJSON1, err := json.Marshal(map[string]any{
			"Version": "2012-10-17",
			"Statement": []map[string]any{
				{
					"Action": []string{
						"sts:AssumeRole",
						"sts:TagSession",
					},
					"Effect": "Allow",
					"Principal": map[string]any{
						"Service": "ec2.amazonaws.com",
					},
				},
			},
		})
		if err != nil {
			return err
		}

		json1 := string(tmpJSON1)
		nodeRole, err := iam.NewRole(ctx, "node-role", &iam.RoleArgs{
			Name:             pulumi.String("nodeRole"),
			AssumeRolePolicy: pulumi.String(json1),
			ManagedPolicyArns: pulumi.StringArray{
				pulumi.String("arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy"),
				pulumi.String("arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy"),
				pulumi.String("arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly"),
				pulumi.String("arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"),
			},
		})
		if err != nil {
			return err
		}

		// nodeInstanceProfile, err := iam.NewInstanceProfile(
		_, err = iam.NewInstanceProfile(
			ctx,
			"node-instance-profile",
			&iam.InstanceProfileArgs{
				Role: nodeRole.Name,
			},
		)

		// nodeSecurityGroup, err := ec2.NewSecurityGroup(
		_, err = ec2.NewSecurityGroup(
			ctx,
			"node-security-group",
			&ec2.SecurityGroupArgs{
				Description: pulumi.String("Security group for all nodes in the cluster"),
				VpcId:       pulumi.StringPtr(vpc.Id),
				Tags: pulumi.StringMap{
					"kubernetes.io/cluster/demo-eks": pulumi.String("owned"),
				},
				Ingress: ec2.SecurityGroupIngressArray{
					ec2.SecurityGroupIngressArgs{
						Description: pulumi.StringPtr("Allow node to communicate with each other"),
						FromPort:    pulumi.Int(0),
						ToPort:      pulumi.Int(0),
						Protocol:    pulumi.String("-1"),
						Self:        pulumi.BoolPtr(true),
					},
					ec2.SecurityGroupIngressArgs{
						Description: pulumi.StringPtr(
							"Allow worker Kubelets and pods to receive communication from the cluster control plane",
						),
						FromPort:       pulumi.Int(1025),
						Protocol:       pulumi.String("tcp"),
						ToPort:         pulumi.Int(65535),
						SecurityGroups: eksCluster.VpcConfig.SecurityGroupIds(),
					},
					ec2.SecurityGroupIngressArgs{
						Description: pulumi.StringPtr(
							"Allow pods running extension API servers on port 443 to receive communication from cluster control plane",
						),
						FromPort:       pulumi.Int(443),
						ToPort:         pulumi.Int(443),
						SecurityGroups: eksCluster.VpcConfig.SecurityGroupIds(),
						Protocol:       pulumi.String("tcp"),
					},
				},
			},
		)

		// _, err = ec2.NewSecurityGroupRule(ctx, "cluster-sg-rule-1", &ec2.SecurityGroupRuleArgs{})
		return nil
	})
}

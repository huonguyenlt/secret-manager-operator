package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/aws-iam-authenticator/pkg/token"
)

// Configuration from environment variables
type Config struct {
	EKSClusterName string
	Region         string
	SecretPrefix   string // Naming convention prefix (e.g., "eks-sync-")
	Namespace      string // Default kubernetes namespace
}

// SecretsManagerEvent represents the CloudWatch Event from Secrets Manager
type SecretsManagerEventDetail struct {
	EventSource       string `json:"eventSource"`
	EventName         string `json:"eventName"`
	RequestParameters struct {
		SecretId string `json:"secretId"`
	} `json:"requestParameters"`
}

func main() {
	lambda.Start(HandleRequest)
}

// HandleRequest is the Lambda handler function
func HandleRequest(ctx context.Context, event events.CloudWatchEvent) error {
	fmt.Printf("Processing event: %s\n", event.DetailType)

	// Step 1: Load configuration from environment variables
	cfg := loadConfig()

	// Step 2: Parse the CloudWatch event to get secret details
	var detail SecretsManagerEventDetail
	if err := json.Unmarshal(event.Detail, &detail); err != nil {
		return fmt.Errorf("failed to parse event detail: %w", err)
	}

	secretName := detail.RequestParameters.SecretId
	fmt.Printf("Secret name from event: %s\n", secretName)

	// Step 3: Check if secret matches naming convention
	if !strings.HasPrefix(secretName, cfg.SecretPrefix) {
		fmt.Printf("Secret '%s' does not match prefix '%s', skipping\n", secretName, cfg.SecretPrefix)
		return nil
	}

	// Step 4: Retrieve secret value from AWS Secrets Manager
	secretData, err := getSecretFromAWS(ctx, cfg.Region, secretName)
	if err != nil {
		return fmt.Errorf("failed to get secret from AWS: %w", err)
	}

	// Step 5: Initialize Kubernetes client for EKS
	k8sClient, err := getKubernetesClient(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Step 6: Update the Kubernetes secret with the same name
	if err := updateKubernetesSecret(ctx, k8sClient, cfg.Namespace, secretName, secretData); err != nil {
		return fmt.Errorf("failed to update kubernetes secret: %w", err)
	}

	fmt.Printf("Successfully synced secret '%s' to EKS cluster\n", secretName)
	return nil
}

// Step 1: Load configuration from environment variables
func loadConfig() Config {
	cfg := Config{
		EKSClusterName: os.Getenv("EKS_CLUSTER_NAME"),
		Region:         os.Getenv("AWS_REGION"),
		SecretPrefix:   os.Getenv("SECRET_PREFIX"),
		Namespace:      os.Getenv("K8S_NAMESPACE"),
	}

	// Set defaults
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.SecretPrefix == "" {
		cfg.SecretPrefix = "eks-sync-"
	}
	if cfg.Namespace == "" {
		cfg.Namespace = "default"
	}

	if cfg.EKSClusterName == "" {
		panic("EKS_CLUSTER_NAME environment variable is required")
	}

	fmt.Printf("Configuration loaded: ClusterName=%s, Region=%s, Prefix=%s, Namespace=%s\n",
		cfg.EKSClusterName, cfg.Region, cfg.SecretPrefix, cfg.Namespace)

	return cfg
}

// Step 4: Retrieve secret value from AWS Secrets Manager
func getSecretFromAWS(ctx context.Context, region, secretName string) (map[string][]byte, error) {
	fmt.Printf("Retrieving secret '%s' from AWS Secrets Manager\n", secretName)

	// Load AWS SDK configuration
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create Secrets Manager client
	svc := secretsmanager.NewFromConfig(awsCfg)

	// Get the secret value
	result, err := svc.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: &secretName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get secret value: %w", err)
	}

	// Parse secret string as JSON (common format)
	secretData := make(map[string][]byte)
	if result.SecretString != nil {
		var jsonData map[string]interface{}
		if err := json.Unmarshal([]byte(*result.SecretString), &jsonData); err != nil {
			// If not JSON, store as single key-value
			secretData["secret"] = []byte(*result.SecretString)
		} else {
			// Convert JSON to map[string][]byte for Kubernetes secret
			for key, value := range jsonData {
				secretData[key] = []byte(fmt.Sprintf("%v", value))
			}
		}
	} else if result.SecretBinary != nil {
		// Handle binary secrets
		secretData["secret"] = result.SecretBinary
	}

	fmt.Printf("Retrieved secret with %d key(s)\n", len(secretData))
	return secretData, nil
}

// Step 5: Initialize Kubernetes client for EKS
func getKubernetesClient(ctx context.Context, cfg Config) (*kubernetes.Clientset, error) {
	fmt.Printf("Connecting to EKS cluster '%s'\n", cfg.EKSClusterName)

	// Load AWS SDK configuration
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cfg.Region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Get EKS cluster information
	eksClient := eks.NewFromConfig(awsCfg)
	clusterOutput, err := eksClient.DescribeCluster(ctx, &eks.DescribeClusterInput{
		Name: &cfg.EKSClusterName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe EKS cluster: %w", err)
	}

	cluster := clusterOutput.Cluster

	// Generate authentication token using aws-iam-authenticator
	gen, err := token.NewGenerator(true, false)
	if err != nil {
		return nil, fmt.Errorf("failed to create token generator: %w", err)
	}

	tok, err := gen.Get(cfg.EKSClusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to get authentication token: %w", err)
	}

	// Create Kubernetes rest config
	k8sConfig := &rest.Config{
		Host:        *cluster.Endpoint,
		BearerToken: tok.Token,
		TLSClientConfig: rest.TLSClientConfig{
			CAData: cluster.CertificateAuthority.Data,
		},
	}

	// Create Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	fmt.Println("Successfully connected to EKS cluster")
	return clientset, nil
}

// Step 6: Update or create Kubernetes secret with the same name
func updateKubernetesSecret(ctx context.Context, client *kubernetes.Clientset, namespace, secretName string, data map[string][]byte) error {
	fmt.Printf("Updating Kubernetes secret '%s' in namespace '%s'\n", secretName, namespace)

	secretsClient := client.CoreV1().Secrets(namespace)

	// Check if secret already exists
	existingSecret, err := secretsClient.Get(ctx, secretName, metav1.GetOptions{})

	if err != nil {
		// Secret doesn't exist, create it
		fmt.Printf("Secret '%s' not found, creating new secret\n", secretName)

		secret := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: namespace,
				Labels: map[string]string{
					"managed-by": "aws-secrets-sync-lambda",
				},
			},
			Type: v1.SecretTypeOpaque,
			Data: data,
		}

		_, err = secretsClient.Create(ctx, secret, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create secret: %w", err)
		}

		fmt.Printf("Successfully created secret '%s'\n", secretName)
	} else {
		// Secret exists, update it
		fmt.Printf("Secret '%s' found, updating existing secret\n", secretName)

		existingSecret.Data = data

		_, err = secretsClient.Update(ctx, existingSecret, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update secret: %w", err)
		}

		fmt.Printf("Successfully updated secret '%s'\n", secretName)
	}

	return nil
}

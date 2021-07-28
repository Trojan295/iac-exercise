package main

import (
	"context"
	"log"
	"time"

	"github.com/Trojan295/iac-exercise/pkg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	"github.com/spf13/cobra"
)

const userData = "IyEvYmluL2Jhc2ggLXhlCmFtYXpvbi1saW51eC1leHRyYXMgaW5zdGFsbCAteSBuZ2lueDEKClRPS0VOPWBjdXJsIC1YIFBVVCAiaHR0cDovLzE2OS4yNTQuMTY5LjI1NC9sYXRlc3QvYXBpL3Rva2VuIiAtSCAiWC1hd3MtZWMyLW1ldGFkYXRhLXRva2VuLXR0bC1zZWNvbmRzOiAyMTYwMCJgCgpjYXQgPDxFT0YgPiAvdXNyL3NoYXJlL25naW54L2h0bWwvaW5kZXguaHRtbApEYXRlOiAkKGRhdGUpCkFNSSBJRDogJChjdXJsIC1IICJYLWF3cy1lYzItbWV0YWRhdGEtdG9rZW46ICRUT0tFTiIgaHR0cDovLzE2OS4yNTQuMTY5LjI1NC9sYXRlc3QvbWV0YS1kYXRhL2FtaS1pZCkKRU9GCgpzeXN0ZW1jdGwgc3RhcnQgbmdpbngKc3lzdGVtY3RsIGVuYWJsZSBuZ2lueAoK"

var rootCmd = &cobra.Command{
	Use:  "deploy",
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		oldAmiID := args[0]
		newAmiID := args[1]

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		cfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			log.Fatalf("error loading AWS configuration: %v\n", err)
		}

		ec2Client := ec2.NewFromConfig(cfg)
		elbClient := elasticloadbalancing.NewFromConfig(cfg)
		deployer := pkg.NewDeployer(ec2Client, elbClient, aws.String(userData))

		err = deployer.Deploy(ctx, &pkg.DeployInput{
			OldAmiID: oldAmiID,
			NewAmiID: newAmiID,
		})

		if err != nil {
			log.Fatal(err)
		}
	},
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatalln(err)
	}
}

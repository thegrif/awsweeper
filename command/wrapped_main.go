package command

import (
	"flag"
	"fmt"
	"io/ioutil"
	goLog "log"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/cloudetc/awsweeper/resource"
	"github.com/hashicorp/terraform/config"
	"github.com/hashicorp/terraform/terraform"
	"github.com/mitchellh/cli"
	log "github.com/sirupsen/logrus"
	terraformProviderAWS "github.com/terraform-providers/terraform-provider-aws/aws"
)

// WrappedMain is the actual main function that does not exit for acceptance testing purposes
func WrappedMain() int {
	app := "awsweeper"
	version := "v0.4.1"

	set := flag.NewFlagSet(app, 0)
	versionFlag := set.Bool("version", false, "Show version")
	helpFlag := set.Bool("help", false, "Show help")
	dryRunFlag := set.Bool("dry-run", false, "Don't delete anything, just show what would happen")
	forceDeleteFlag := set.Bool("force", false, "Start deleting without asking for confirmation")
	profile := set.String("profile", "", "Use a specific profile from your credential file")
	region := set.String("region", "", "The region to use. Overrides config/env settings")
	maxRetries := set.Int("max-retries", 25, "The maximum number of times an AWS API request is being executed")
	outputType := set.String("output", "string", "The type of output result (String, JSON or YAML) default: String")

	// discard internal logs of Terraform AWS provider
	goLog.SetOutput(ioutil.Discard)

	set.Usage = func() {
		fmt.Println(help())
	}

	err := set.Parse(os.Args[1:])
	if err != nil {
		// the Parse function prints already an error + help message, so we don't want to output it here again
		log.WithError(err).Debug("failed to parse command line arguments")
		return 1
	}

	if *versionFlag {
		fmt.Println(version)
		return 0
	}

	if *helpFlag {
		fmt.Println(help())
		return 0
	}

	c := &cli.CLI{
		Name:     app,
		Version:  version,
		HelpFunc: basicHelpFunc(app),
	}
	c.Args = append([]string{"wipe"}, set.Args()...)

	sess := session.Must(session.NewSessionWithOptions(session.Options{
		Config:            aws.Config{Region: region},
		SharedConfigState: session.SharedConfigEnable,
		Profile:           *profile,
	}))
	log.Infof("using region: %s", *sess.Config.Region)

	p := initAwsProvider(*profile, *sess.Config.Region, *maxRetries)

	ui := &cli.BasicUi{
		Reader:      os.Stdin,
		Writer:      os.Stdout,
		ErrorWriter: os.Stderr,
	}

	client := resource.NewAWS(sess)

	c.Commands = map[string]cli.CommandFactory{
		"wipe": func() (cli.Command, error) {
			return &Wipe{
				UI: &cli.ColoredUi{
					Ui:          ui,
					OutputColor: cli.UiColorBlue,
				},
				client:      client,
				provider:    p,
				dryRun:      *dryRunFlag,
				forceDelete: *forceDeleteFlag,
				outputType:  *outputType,
			}, nil
		},
	}

	exitStatus, err := c.Run()
	if err != nil {
		log.WithError(err).Fatal("failed to run command")
	}

	return exitStatus
}

func help() string {
	return `
Usage: awsweeper [options] <config.yaml>

Delete AWS resources via a yaml configuration.

Options:
  --profile				Use a specific profile from your credential file

  --region				The region to use. Overrides config/env settings

  --dry-run				Don't delete anything, just show what would happen

  --force				Start deleting without asking for confirmation

  --max-retries				The maximum number of times an AWS API request is being executed
  
  --output				The type of output result (string, json or yaml) default: string
`
}

func basicHelpFunc(app string) cli.HelpFunc {
	return func(commands map[string]cli.CommandFactory) string {
		return help()
	}
}

func initAwsProvider(profile string, region string, maxRetries int) *terraform.ResourceProvider {
	p := terraformProviderAWS.Provider()

	cfg := map[string]interface{}{
		"region":      region,
		"profile":     profile,
		"max_retries": maxRetries,
	}

	rc, err := config.NewRawConfig(cfg)
	if err != nil {
		log.WithError(err).Fatalf("failed to create raw Terraform AWS provider config")
	}
	conf := terraform.NewResourceConfig(rc)

	warns, errs := p.Validate(conf)
	if len(warns) > 0 {
		log.Warn(warns)
	}
	if len(errs) > 0 {
		log.Error(errs)
	}

	if err := p.Configure(conf); err != nil {
		log.WithError(err).Fatalf("failed to configure Terraform AWS provider")
	}

	return &p
}

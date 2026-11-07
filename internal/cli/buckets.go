package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/client"
)

// buckets lists, adds, changes and removes the workspace's buckets, and
// picks the bucket of an app.
func buckets(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	command := "ls"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	usage := map[string]string{
		"ls":      "buckets",
		"add":     "buckets add <name> --endpoint https://… --bucket <bucket> --access-key <key> [--region auto] [--path <folder>] [--default], then the secret key on stdin",
		"edit":    "buckets edit <name> [--name <new name>] [--endpoint …] [--bucket …] [--path …] [--region …] [--access-key … or --secret, then the secret key on stdin]",
		"default": "buckets default <name>",
		"rm":      "buckets rm <name> [--yes]",
		"use":     "buckets use <bucket> [app], or buckets use --default [app]",
	}[command]
	if usage == "" {
		return fmt.Errorf("unknown command %q: use %s buckets, buckets add, edit, default, rm or use", command, build.Name)
	}
	flags := newFlags("buckets "+command, usage)
	var endpoint, region, bucket, folder, accessKey, rename *string
	var makeDefault, newSecret *bool
	switch command {
	case "add", "edit":
		endpoint = flags.String("endpoint", "", "https:// URL of an S3-compatible store; empty for AWS S3")
		region = flags.String("region", "", "region; auto for Cloudflare R2")
		bucket = flags.String("bucket", "", "the bucket's name at your storage provider")
		folder = flags.String("path", "", "a folder inside the bucket, only if it holds other files too")
		accessKey = flags.String("access-key", "", "access key ID")
		makeDefault = flags.Bool("default", false, "make it the default bucket of the workspace")
		if command == "edit" {
			rename = flags.String("name", "", "a new name in "+build.Name)
			newSecret = flags.Bool("secret", false, "read a new secret key on stdin")
		}
	case "rm":
		flags.withYes()
	case "use":
		flags.withApp()
		makeDefault = flags.Bool("default", false, "follow the workspace's default bucket")
	}
	if err := flags.parse(args); err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	switch command {
	case "ls":
		if flags.NArg() > 0 {
			return flags.wrong()
		}
		list, err := api.Buckets(ctx)
		if err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Fprintf(stdout, "No bucket yet. Add one: %s buckets add default --endpoint https://… --bucket <bucket> --access-key <key>\n", build.Name)
			return nil
		}
		rows := [][]string{}
		for _, item := range list {
			name := item.Name
			if item.Default {
				name += " (default)"
			}
			apps := strings.Join(item.Apps, ", ")
			if len(item.OlderBackups) > 0 {
				apps = strings.TrimPrefix(apps+"; left by "+strings.Join(item.OlderBackups, ", "), "; ")
			}
			rows = append(rows, []string{name, item.Bucket, item.Folder, orAWS(item.Endpoint), item.AccessKey, dash(apps)})
		}
		table(stdout, []string{"NAME", "BUCKET", "FOLDER", "ENDPOINT", "ACCESS KEY", "APPS"}, rows)
		return nil
	case "add":
		if flags.NArg() != 1 {
			return flags.wrong()
		}
		if *bucket == "" || *accessKey == "" {
			return fmt.Errorf("give --bucket and --access-key; the secret key comes on stdin: %s", flags.usage)
		}
		secret, err := askSecret(stdin, stdout)
		if err != nil {
			return err
		}
		if secret == "" {
			return errors.New("the secret key is empty")
		}
		added, err := api.AddBucket(ctx, client.BucketInput{Name: flags.Arg(0), Endpoint: *endpoint, Region: *region, Bucket: *bucket, Path: *folder, AccessKey: *accessKey, SecretKey: secret, Default: *makeDefault})
		if err != nil {
			return err
		}
		if added.Default {
			done(stdout, "Added %s, the default bucket: uploads and backups of every app without a bucket of its own go there.", added.Name)
			return nil
		}
		done(stdout, "Added %s. Move an app there with %s buckets use %s <app>.", added.Name, build.Name, added.Name)
		return nil
	case "edit", "default":
		if flags.NArg() != 1 {
			return flags.wrong()
		}
		current, err := api.Bucket(ctx, flags.Arg(0))
		if err != nil {
			return err
		}
		input := current.Input()
		if command == "default" {
			input.Default = true
		} else {
			given := map[string]bool{}
			flags.Visit(func(f *flag.Flag) { given[f.Name] = true })
			for name, value := range map[string]*string{"endpoint": endpoint, "region": region, "bucket": bucket, "path": folder, "access-key": accessKey, "name": rename} {
				if !given[name] {
					continue
				}
				switch name {
				case "endpoint":
					input.Endpoint = *value
				case "region":
					input.Region = *value
				case "bucket":
					input.Bucket = *value
				case "path":
					input.Path = *value
				case "access-key":
					input.AccessKey = *value
				case "name":
					input.Name = *value
				}
			}
			input.Default = current.Default || *makeDefault
			// A new access key comes with its secret key.
			if given["access-key"] || *newSecret {
				input.SecretKey, err = askSecret(stdin, stdout)
				if err != nil {
					return err
				}
				if input.SecretKey == "" {
					return errors.New("the secret key is empty")
				}
			}
		}
		changed, err := api.EditBucket(ctx, current.Name, input)
		if err != nil {
			return err
		}
		if command == "default" {
			done(stdout, "%s is the default bucket now. Apps without a bucket of their own upload and back up there; their history starts again there.", changed.Name)
			return nil
		}
		done(stdout, "Saved %s.", changed.Name)
		return nil
	case "rm":
		if flags.NArg() != 1 {
			return flags.wrong()
		}
		name := flags.Arg(0)
		err = confirmName(stdin, stdout, flags.yes(), "Remove the bucket "+name+" from "+build.Name+"? What is in it stays.", name)
		if err != nil {
			return err
		}
		err = api.RemoveBucket(ctx, name)
		if err != nil {
			return err
		}
		done(stdout, "Removed %s. What is in it stays.", name)
		return nil
	case "use":
		target, argument := "", ""
		switch {
		case *makeDefault && flags.NArg() <= 1:
			argument = flags.Arg(0)
		case !*makeDefault && flags.NArg() >= 1 && flags.NArg() <= 2:
			target, argument = flags.Arg(0), flags.Arg(1)
		default:
			return flags.wrong()
		}
		name, err := flags.app(argument)
		if err != nil {
			return err
		}
		app, err := api.SetAppBucket(ctx, name, target)
		if err != nil {
			return err
		}
		to := app.Bucket
		if to == "" {
			to = "the default bucket"
		}
		done(stdout, "Uploads and backups of %s go to %s now.", app.Name, to)
		fmt.Fprintf(stdout, "  The history starts again in the new bucket; what is in the old one stays there.\n")
		return nil
	}
	return nil
}

// askSecret reads a secret key from stdin, asking for it on a terminal.
func askSecret(stdin io.Reader, stdout io.Writer) (string, error) {
	if isTerminal(stdin) {
		fmt.Fprint(stdout, "Secret key: ")
	}
	secret, err := readSecret(stdin)
	if isTerminal(stdin) {
		fmt.Fprintln(stdout)
	}
	return secret, err
}

func orAWS(endpoint string) string {
	if endpoint == "" {
		return "AWS S3"
	}
	return endpoint
}

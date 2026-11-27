package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/azamatbayramov/referflow/backend/internal/auth"
	"github.com/azamatbayramov/referflow/backend/internal/config"
	"github.com/azamatbayramov/referflow/backend/internal/postgres"
	"github.com/azamatbayramov/referflow/backend/migrations"
)

func main() {
	if err := run(os.Args[1:], os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, input io.Reader) error {
	if len(args) == 0 {
		return errors.New("usage: manage migrate | create-user --email EMAIL --name NAME --password-stdin")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	email := flags.String("email", "", "account email")
	name := flags.String("name", "", "display name")
	stdin := flags.Bool("password-stdin", false, "read password from stdin")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		return errors.New("invalid command arguments")
	}
	if args[0] != "migrate" && args[0] != "create-user" {
		return errors.New("unknown management command")
	}
	if args[0] == "create-user" && (!*stdin || *email == "" || *name == "") {
		return errors.New("email, name and --password-stdin are required")
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, err := postgres.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if args[0] == "migrate" {
		if err := migrations.Up(ctx, pool); err != nil {
			return err
		}
		fmt.Println("Migrations are up to date.")
		return nil
	}
	password, err := io.ReadAll(io.LimitReader(input, 514))
	if err != nil || len(password) > 513 {
		return errors.New("cannot read password; maximum 128 characters")
	}
	secret := strings.TrimSuffix(strings.TrimSuffix(string(password), "\n"), "\r")
	if err := (auth.Store{Pool: pool}).CreateAccount(ctx, *email, *name, secret); err != nil {
		return err
	}
	fmt.Println("Account created.")
	return nil
}

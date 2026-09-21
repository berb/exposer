package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/berb/exposer"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func runValidate(args []string) {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	schemaPath := fs.String("schema", "", "schema to validate against (default: the built-in one)")
	docPath := fs.String("document", "target/index.json", "document to validate")
	fs.Parse(args)

	var schemaDoc io.ReadCloser
	var err error
	if *schemaPath == "" {
		*schemaPath = builtinSchema
		schemaDoc, err = exposer.Files.Open(builtinSchema)
	} else {
		schemaDoc, err = os.Open(*schemaPath)
	}
	if err != nil {
		fail("cannot read schema %s: %v", *schemaPath, err)
	}
	defer schemaDoc.Close()

	compiler := jsonschema.NewCompiler()
	parsed, err := jsonschema.UnmarshalJSON(schemaDoc)
	if err != nil {
		fail("cannot parse schema %s: %v", *schemaPath, err)
	}
	if err := compiler.AddResource(*schemaPath, parsed); err != nil {
		fail("cannot load schema %s: %v", *schemaPath, err)
	}
	schema, err := compiler.Compile(*schemaPath)
	if err != nil {
		fail("cannot compile schema %s: %v", *schemaPath, err)
	}

	file, err := os.Open(*docPath)
	if err != nil {
		fail("cannot read %s: %v", *docPath, err)
	}
	defer file.Close()
	instance, err := jsonschema.UnmarshalJSON(file)
	if err != nil {
		fail("cannot parse %s: %v", *docPath, err)
	}
	if err := schema.Validate(instance); err != nil {
		fail("%s does not match %s:\n%v", *docPath, *schemaPath, err)
	}
	fmt.Printf("%s validates against %s\n", *docPath, *schemaPath)
}

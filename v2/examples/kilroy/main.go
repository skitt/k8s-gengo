/*
Copyright 2023 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// kilroy is a trivial gengo/v2 program which adds a tag-method to types.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/spf13/pflag"
	"k8s.io/gengo/v2/args"
	"k8s.io/gengo/v2/generator"
	"k8s.io/gengo/v2/namer"
	"k8s.io/gengo/v2/types"
	"k8s.io/klog/v2"
)

func main() {
	klog.InitFlags(nil)
	stdArgs, myArgs := getArgs()

	// Collect and parse flags.
	stdArgs.AddFlags(pflag.CommandLine)
	myArgs.AddFlags(pflag.CommandLine)
	flag.Set("logtostderr", "true")
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	if err := validateArgs(stdArgs); err != nil {
		klog.ErrorS(err, "fatal error")
		os.Exit(1)
	}

	// Run the tool.
	if err := stdArgs.Execute(
		getNameSystems(),
		getDefaultNameSystem(),
		getTargets,
		args.StdBuildTag,
		pflag.Args(),
	); err != nil {
		klog.ErrorS(err, "fatal error")
		os.Exit(1)
	}
	klog.V(2).InfoS("completed successfully")
}

// toolArgs is used by the gengo framework to pass args specific to this generator.
type toolArgs struct {
	outputFile   string
	methodName   string
	goHeaderFile string
}

// getArgs returns default arguments for the generator.
func getArgs() (*args.GeneratorArgs, *toolArgs) {
	stdArgs := args.Default()
	toolArgs := &toolArgs{}
	stdArgs.CustomArgs = toolArgs
	return stdArgs, toolArgs
}

// AddFlags adds this tool's flags to the flagset.
func (ta *toolArgs) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&ta.outputFile, "output-file", "generated.kilroy.go",
		"the name of the file to be generated")
	fs.StringVar(&ta.methodName, "method-name", "KilroyWasHere",
		"the name of the method to add")
	fs.StringVar(&ta.goHeaderFile, "go-header-file", "",
		"the path to a file containing boilerplate header text; the string \"YEAR\" will be replaced with the current 4-digit year")
}

// validateArgs checks the given arguments.
func validateArgs(stdArgs *args.GeneratorArgs) error {
	toolArgs := stdArgs.CustomArgs.(*toolArgs)
	if len(toolArgs.outputFile) == 0 {
		return fmt.Errorf("--output-file must be specified")
	}
	if len(toolArgs.methodName) == 0 {
		return fmt.Errorf("--method-name must be specified")
	}

	return nil
}

// getNameSystems returns the name system used by the generators in this package.
func getNameSystems() namer.NameSystems {
	return namer.NameSystems{
		"raw": namer.NewRawNamer("", nil),
	}
}

// getDefaultNameSystem returns the default name system for ordering the types to be
// processed by the generators in this package.
func getDefaultNameSystem() string {
	return "raw"
}

// getTargets is called after the inputs have been loaded.  It is expected to
// examine the provided context and return a list of Packages which will be
// executed further.
func getTargets(c *generator.Context, arguments *args.GeneratorArgs) []generator.Target {
	toolArgs := arguments.CustomArgs.(*toolArgs)

	boilerplate, err := args.GoBoilerplate(toolArgs.goHeaderFile, args.StdBuildTag, args.StdGeneratedBy)
	if err != nil {
		klog.Fatalf("failed loading boilerplate: %v", err)
	}

	targets := []generator.Target{}
	for _, input := range c.Inputs {
		klog.V(2).InfoS("processing", "pkg", input)

		pkg := c.Universe[input]
		if pkg == nil { // e.g. the input had no Go files
			continue
		}

		targets = append(targets, &generator.SimpleTarget{
			PkgName:       pkg.Name,
			PkgPath:       pkg.Path,       // output pkg is the same as the input
			PkgDir:        pkg.SourcePath, // output pkg is the same as the input
			HeaderComment: boilerplate,

			// FilterFunc returns true if this Package cares about this type.
			// Each Generator has its own Filter method which will be checked
			// subsequently.  This will be called for every type in every
			// loaded package, not just things in our inputs.
			FilterFunc: func(c *generator.Context, t *types.Type) bool {
				return t.Name.Package == pkg.Path
			},

			// GeneratorsFunc returns a list of Generators, each of which is
			// responsible for a single output file (though multiple generators
			// may write to the same one).
			GeneratorsFunc: func(c *generator.Context) (generators []generator.Generator) {
				return []generator.Generator{
					newKilroyGenerator(toolArgs.outputFile, pkg, toolArgs.methodName),
				}
			},
		})
	}

	return targets
}

// kilroyGenerator produces a file with autogenerated functions.
type kilroyGenerator struct {
	generator.GolangGenerator
	myPackage  *types.Package
	methodName string
}

func newKilroyGenerator(outputFilename string, pkg *types.Package, methodName string) generator.Generator {
	return &kilroyGenerator{
		GolangGenerator: generator.GolangGenerator{
			OutputFilename: outputFilename,
		},
		myPackage:  pkg,
		methodName: methodName,
	}
}

// Filter returns true if this Generator cares about this type.
// This will be called for every type which made it through this Package's
// Filter method.
func (g *kilroyGenerator) Filter(c *generator.Context, t *types.Type) bool {
	// We only handle exported structs.
	return t.Kind == types.Struct && !namer.IsPrivateGoName(t.Name.Name)
}

// Namers returns a set of NameSystems which will be merged with the namers
// provided when executing this package. In case of a name collision, the
// values produced here will win.
func (g *kilroyGenerator) Namers(*generator.Context) namer.NameSystems {
	return namer.NameSystems{
		// This elides package names when the name is in "this" package.
		"raw": namer.NewRawNamer(g.myPackage.Path, nil),
	}
}

// GenerateType should emit code for the specified type.  This will be called
// for every type which made it through this Generator's Filter method.
func (g *kilroyGenerator) GenerateType(c *generator.Context, t *types.Type, w io.Writer) error {
	klog.V(2).InfoS("generating kilroy method", "type", t.String(), "method", g.methodName)

	sw := generator.NewSnippetWriter(w, c, "$", "$")
	args := argsFromType(t)
	args["methodName"] = g.methodName

	sw.Do("// $.methodName$ is an autogenerated method.\n", args)
	sw.Do("func ($.type|raw$) $.methodName$() {}\n", args)

	return sw.Error()
}

func argsFromType(ts ...*types.Type) generator.Args {
	a := generator.Args{
		"type": ts[0],
	}
	for i, t := range ts {
		a[fmt.Sprintf("type%d", i+1)] = t
	}
	return a
}

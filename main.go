package main

import (
	"fmt"
	"os"

	"flag"

	"github.com/qubesome/qubesome-cli/internal/qubesome"
)

func main() {

	// if os.Args < 2 {
	//     fmt.Printf("Usage: %s\n", os.Args[0])
	//     os.Exit(1)
	// }

	in := qubesome.WorkloadInfo{}

	flag.StringVar(&in.Name, "name", "",
		fmt.Sprintf("The name of the workload to be executed. For new workloads use %s import first.", os.Args[0]))
	flag.StringVar(&in.Profile, "profile", "untrusted", "The profile name which will be used to run the workload.")
	flag.Parse()

	q := qubesome.New()
	err := q.Run(in)
	checkNil(err)
}

func checkNil(err error) error {
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	return nil
}

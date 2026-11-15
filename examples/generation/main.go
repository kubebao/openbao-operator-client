// Command generation demonstrates generation acknowledgement without Kubernetes access.
package main

import (
	"fmt"

	"github.com/kubebao/openbao-operator-client/evidence"
)

func main() {
	fmt.Println(evidence.CompareGeneration(new(int64(3)), new(int64(2))))
}

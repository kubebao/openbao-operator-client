// Command generation demonstrates generation acknowledgement without Kubernetes access.
package main

import (
	"fmt"

	"github.com/kubebao/openbao-operator-client/evidence"
)

func main() {
	generation, observed := int64(3), int64(2)
	fmt.Println(evidence.CompareGeneration(&generation, &observed))
}

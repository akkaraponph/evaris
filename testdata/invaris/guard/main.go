// Command guard is an original Invaris structured command guard used only by
// Evaris tests. It inspects result.txt in its working directory and writes one
// invaris.inspection/v1alpha1 object: "approved\n" is clean (exit 0),
// "rejected\n" is one finding (exit 1), and anything else refuses (exit 2).
package main

import (
	"fmt"
	"os"
)

const policy = "approved-result"

func main() {
	b, err := os.ReadFile("result.txt")
	switch {
	case err == nil && string(b) == "approved\n":
		fmt.Printf(`{"version":"invaris.inspection/v1alpha1","policyId":%q,"inspected":1,"findings":[]}`+"\n", policy)
	case err == nil && string(b) == "rejected\n":
		fmt.Printf(`{"version":"invaris.inspection/v1alpha1","policyId":%q,"inspected":1,"findings":[{"policyId":%q,"path":"result.txt"}]}`+"\n", policy, policy)
		os.Exit(1)
	default:
		fmt.Fprintln(os.Stderr, "result.txt missing or not a recognized control")
		os.Exit(2)
	}
}

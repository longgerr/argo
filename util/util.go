package util

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/fields"

	log "github.com/sirupsen/logrus"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"github.com/argoproj/argo/v2/errors"
	wfv1 "github.com/argoproj/argo/v2/pkg/apis/workflow/v1alpha1"
	errorsutil "github.com/argoproj/argo/v2/util/errors"
	"github.com/argoproj/argo/v2/util/retry"
)

type Closer interface {
	Close() error
}

// Close is a convenience function to close a object that has a Close() method, ignoring any errors
// Used to satisfy errcheck lint
func Close(c Closer) {
	_ = c.Close()
}

// GetSecrets retrieves a secret value and memoizes the result
func GetSecrets(ctx context.Context, clientSet kubernetes.Interface, namespace, name, key string) ([]byte, error) {

	secretsIf := clientSet.CoreV1().Secrets(namespace)
	var secret *apiv1.Secret
	var err error
	_ = wait.ExponentialBackoff(retry.DefaultRetry, func() (bool, error) {
		secret, err = secretsIf.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			log.Warnf("Failed to get secret '%s': %v", name, err)
			if !errorsutil.IsTransientErr(err) {
				return false, err
			}
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return []byte{}, errors.InternalWrapError(err)
	}
	val, ok := secret.Data[key]
	if !ok {
		return []byte{}, errors.Errorf(errors.CodeBadRequest, "secret '%s' does not have the key '%s'", name, key)
	}
	return val, nil
}

// Write the Terminate message in pod spec
func WriteTeriminateMessage(message string) {
	err := ioutil.WriteFile("/dev/termination-log", []byte(message), 0644)
	if err != nil {
		panic(err)
	}
}

// Merge the two parameters Slice
// Merge the slices based on arguments order (first is high priority).
func MergeParameters(params ...[]wfv1.Parameter) []wfv1.Parameter {
	var resultParams []wfv1.Parameter
	passedParams := make(map[string]bool)
	for _, param := range params {
		for _, item := range param {
			if _, ok := passedParams[item.Name]; ok {
				continue
			}
			resultParams = append(resultParams, item)
			passedParams[item.Name] = true
		}
	}
	return resultParams
}

// MergeArtifacts merges artifact argument slices
// Merge the slices based on arguments order (first is high priority).
func MergeArtifacts(artifactSlices ...[]wfv1.Artifact) []wfv1.Artifact {
	var result []wfv1.Artifact
	alreadyMerged := make(map[string]bool)
	for _, artifacts := range artifactSlices {
		for _, item := range artifacts {
			if !alreadyMerged[item.Name] {
				result = append(result, item)
				alreadyMerged[item.Name] = true
			}
		}
	}
	return result
}

func RecoverIndexFromNodeName(name string) int {
	startIndex := strings.Index(name, "(")
	endIndex := strings.Index(name, ":")
	if startIndex < 0 || endIndex < 0 {
		return -1
	}
	out, err := strconv.Atoi(name[startIndex+1 : endIndex])
	if err != nil {
		return -1
	}
	return out
}

func GenerateFieldSelectorFromWorkflowName(wfName string) string {
	result := fields.ParseSelectorOrDie(fmt.Sprintf("metadata.name=%s", wfName)).String()
	compare := RecoverWorkflowNameFromSelectorStringIfAny(result)
	if wfName != compare {
		panic(fmt.Sprintf("Could not recover field selector from workflow name. Expected '%s' but got '%s'\n", wfName, compare))
	}
	return result
}

func RecoverWorkflowNameFromSelectorStringIfAny(selector string) string {
	const tag = "metadata.name="
	if starts := strings.Index(selector, tag); starts > -1 {
		suffix := selector[starts+len(tag):]
		if ends := strings.Index(suffix, ","); ends > -1 {
			return strings.TrimSpace(suffix[:ends])
		}
		return strings.TrimSpace(suffix)
	}
	return ""
}

// getDeletePropagation return the default or configured DeletePropagation policy
func GetDeletePropagation() *metav1.DeletionPropagation {
	propagationPolicy := metav1.DeletePropagationBackground
	envVal, ok := os.LookupEnv("WF_DEL_PROPAGATION_POLICY")
	if ok && envVal != "" {
		propagationPolicy = metav1.DeletionPropagation(envVal)
	}
	return &propagationPolicy
}

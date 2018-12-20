package testproject

import (
	"fmt"
	"github.com/pkg/errors"
	apps_v1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"log"
	"reflect"
	"sort"
	"strconv"
	"time"
)

func Run() {
	fmt.Println("It works")
}

const SPARE_DEPLOYS_ANNOTATION_KEY = "foo.com/spares"
const OBJECT_NAME_KEY = "app.kubernetes.io/name"
const OBJECT_VERSION_KEY = "app.kubernetes.io/version"

// GitRef pulls a git ref out of a k8s object.  The first 8 characters of the git hash.  This will either be in the annotations or in the name for legacy deployments.
func GitRef(thing GenericK8sObj) (gitRef string, err error) {

	gitRef = thing.GetLabels()[OBJECT_VERSION_KEY]

	// Got the ref, return it
	if gitRef != "" {
		return gitRef, err
	}

	return gitRef, err
}

type K8sDeployer struct {
	Name                  string
	AppDir                string
	Namespace             string
	ManifestFiles         []ManifestFile
	TempDir               string
	GitRef                string
	Deployments           []*apps_v1.Deployment
	StatefulSets          []*apps_v1.StatefulSet
	DaemonSets            []*apps_v1.DaemonSet
	MaxReserveDeployments int
	Verbose               bool
	Debug                 bool
	ClientSet             *kubernetes.Clientset
	ConfigFile            string
	SecretPaths           []string
}

// ManifestFile contains a read file, it's raw contents, and it's 'completed' contents (variables and secrets filled in)
type ManifestFile struct {
	FileName          string
	RawContents       string
	CompletedContents string
}

// GitRefTime holds a name/version string of format name-:-version, a git ref, and it's object creation timestamp
type GitRefTime struct {
	Name      string
	Ref       string
	Timestamp time.Time
}

// GitRefSlice is a list of DepRefTimes
type GitRefSlice []GitRefTime

func (grs *GitRefSlice) Contains(gitRef string) bool {

	for _, ref := range *grs {
		if ref.Ref == gitRef {
			return true
		}
	}

	return false
}
func (grs *GitRefSlice) String() string {
	output := "[\n"
	for _, ref := range *grs {
		output += fmt.Sprintf("  %s: %s\n", ref.Ref, ref.Timestamp.String())
	}
	output += "]"

	return output
}

// Len returns the length of the GitRefSlice
func (d GitRefSlice) Len() int {
	return len(d)
}

// Less returns whether the 2nd deployment is before the first
func (d GitRefSlice) Less(i, j int) bool {
	return d[j].Timestamp.Before(d[i].Timestamp)
}

// Swap swaps position in the slice for two GitRefTime objects
func (d GitRefSlice) Swap(i, j int) {
	d[i], d[j] = d[j], d[i]
}

// GenericK8sObj is a simple interface that should work for all k8s objects, as they all have Names, Labels, and Annotations
type GenericK8sObj interface {
	GetName() string
	GetLabels() map[string]string
	GetAnnotations() map[string]string
	GetCreationTimestamp() v1.Time
}

// GetSupportedObjects  gets supported objects from the apiserver. Supports Deployments, StatefulSets, DaemonSets, Services, and Ingresses .
func (d *K8sDeployer) GetSupportedObjects() (objects []GenericK8sObj, err error) {
	objects = make([]GenericK8sObj, 0)

	return objects, err
}

// GetLatestGitRef gets the git ref of the latest app deployment.
func (d *K8sDeployer) GetLatestGitRef() (gitRef string, err error) {
	if d.Verbose {
		log.Printf("Getting latest git ref in namespace %q", d.Namespace)
	}

	// initial timestamp
	t := time.Date(2000, time.January, 01, 0, 0, 0, 0, time.UTC)

	objects, err := d.GetSupportedObjects()

	for _, obj := range objects {
		if obj.GetCreationTimestamp().After(t) {
			t = obj.GetCreationTimestamp().Time
			gitRef, err = GitRef(obj)
			if err != nil {
				err = errors.Wrapf(err, "failed to get git ref of object")
				return gitRef, err
			}
		}
	}

	return gitRef, err
}

// GetGitRefsInNamespace returns a list of time sorted deployments, newest to eldest.
func (d *K8sDeployer) GetGitRefsInNamespace(verbose bool) (refs GitRefSlice, err error) {
	// make list of dep refs sorted by time
	depRefs := make(map[string]time.Time)

	objects, err := d.GetSupportedObjects()

	if d.Debug {
		log.Printf("Getting refs in namespace %q", d.Namespace)
		log.Printf("Found %d objects", len(objects))
	}

	for _, obj := range objects {
		if d.Debug {
			log.Printf("Examining Object Name: %s, Type: %s", obj.GetName(), reflect.TypeOf(obj).String())
		}

		ref, err := GitRef(obj)
		if err != nil {
			err = errors.Wrapf(err, "failed to get dep ref of object")
			return refs, err
		}

		if ref != "" { // some objects won't have git refs, such as static deployments.  We just ignore them.
			if d.Debug {
				log.Printf("  Ref: %s", ref)
			}

			refTime, ok := depRefs[ref]
			if ok {
				if refTime.Before(obj.GetCreationTimestamp().Time) {
					depRefs[ref] = obj.GetCreationTimestamp().Time
				}
			} else {
				depRefs[ref] = obj.GetCreationTimestamp().Time
			}
		}
	}

	refs = make(GitRefSlice, 0)

	for k, v := range depRefs {
		drt := GitRefTime{
			Ref:       k,
			Timestamp: v,
		}

		refs = append(refs, drt)
	}

	sort.Sort(refs)

	return refs, err
}

// CleanupOldObjects  cleans up old objects.  Any object can have a 'tail' of old deployments that are allowed to stay up until they pass a configured threshold, after which they're removed.  Supports Deployments, StatefulSets, DaemonSets, Services, and Ingresses.
func (d *K8sDeployer) CleanupOldObjects() (err error) {
	if d.Verbose {
		log.Printf("Cleaning up old resources.")
	}

	return err
}

// DeleteDeployment deletes objects based on it's 8 character Git ref.  Slightly confusing as it has to handle k8s deployments as well as other k8s objects of different names such as services, ingresses and persistent volume claims. Method must be extended for each supported k8s type
func (d *K8sDeployer) DeleteGitRef(gitRef string) (err error) {
	// There is almost certainly d better way to do this.  Due to the way the k8s api is laid out we have to do this per client type.
	// In the future perhaps we can genericize, but this is what we have for now.
	// At present this handles deployments, services, persistent volume claims and ingresses.  Support for anything else will need to be added.

	if d.Verbose {
		log.Printf("Deleting app deployment %s", gitRef)
	}

	return err
}

type DeployedVersion struct {
	Name       string
	Versions   GitRefSlice
	MaxReserve int
}

func (d *K8sDeployer) GenericCleanup(
	objectTypeName string,
	getter func(namespace string, clientset *kubernetes.Clientset, verbose bool) (objects []GenericK8sObj, err error),
	deleter func(objName string, namespace string, clientset *kubernetes.Clientset, verbose bool) (err error),
) (err error) {

	objects, err := getter(d.Namespace, d.ClientSet, d.Verbose)
	if err != nil {
		err = errors.Wrapf(err, "failed to run getter for %s", objectTypeName)
	}

	if d.Verbose {
		log.Printf("Cleaning up old %s", objectTypeName)
		log.Printf("%s in namespace %s", objectTypeName, d.Namespace)
	}

	deployedVersions := make(map[string]*DeployedVersion)

	// build the list of objects
	for _, obj := range objects {
		annotations := obj.GetAnnotations()
		labels := obj.GetLabels()

		spares := annotations[SPARE_DEPLOYS_ANNOTATION_KEY]
		name := labels[OBJECT_NAME_KEY]
		version := labels[OBJECT_VERSION_KEY]
		objName := obj.GetName()

		// deliberately ignore cleanup of objects that do not have recognizable names from labels
		if name == "" {
			continue
		}

		if d.Verbose {
			log.Printf("Found name: %q, version: %q, spares: %q, objName: %q", name, version, spares, objName)
		}

		sparesInt := d.MaxReserveDeployments

		if spares != "" {
			sparesInt, err = strconv.Atoi(spares)
			if err != nil {
				err = errors.Wrapf(err, "failed to convert %s to an int", spares)
				return err
			}
		}

		deployedVersion, ok := deployedVersions[name]
		if ok {
			gitRefTime := GitRefTime{
				Name:      objName,
				Ref:       version,
				Timestamp: obj.GetCreationTimestamp().Time,
			}
			deployedVersion.Versions = append(deployedVersion.Versions, gitRefTime)

		} else {
			gitRefTime := GitRefTime{
				Name:      objName,
				Ref:       version,
				Timestamp: obj.GetCreationTimestamp().Time,
			}
			deployedVersions[name] = &DeployedVersion{
				Name:     name,
				Versions: GitRefSlice{gitRefTime},
			}
		}

		// whatever else is in there, the current git ref's idea of spares is what rules
		if version == d.GitRef {
			deployedVersions[name].MaxReserve = sparesInt
		}
	}

	if d.Verbose {
		log.Printf("%q objects:", objectTypeName)
	}

	for appName, info := range deployedVersions {
		var totalAllowed int
		if info.MaxReserve == 0 {
			totalAllowed = d.MaxReserveDeployments + 1
		} else {
			totalAllowed = info.MaxReserve + 1
		}

		if d.Verbose {
			log.Printf("%s:", appName)

			for _, v := range info.Versions {
				log.Printf("  name: %q ref: %q", v.Name, v.Ref)
			}
		}

		if d.Verbose {
			log.Printf("Total: %d", len(info.Versions))
			log.Printf("Allowed: %d", totalAllowed)
		}

		// no-op if we're under the maxReserve
		if len(info.Versions) <= totalAllowed {
			if d.Verbose {
				log.Printf("All Good.  Moving on.")
			}
			continue
		}

		if d.Verbose {
			log.Printf("Too many.  Gots to delete some.")
		}

		sort.Sort(info.Versions)

		if d.Verbose {
			log.Printf("Time sorted resources:")
			for _, dep := range info.Versions {
				log.Printf("  Name/Version: %q Timestamp: %q", dep.Name, dep.Timestamp)
			}
		}

		toBeDeleted := info.Versions[totalAllowed:]

		for _, version := range toBeDeleted {
			err = deleter(version.Name, d.Namespace, d.ClientSet, d.Verbose)
			if err != nil {
				err = errors.Wrapf(err, "failed to delete object %q", version.Name)
				return err
			}
		}
	}

	return err
}

// TODO make ClientFor(obj GenericK8sObj) to return proper client for a given object.
// If we can do this, it *might* be all the 'generic' we need to create and destroy things.

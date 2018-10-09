package main

import (
	"fmt"

	"k8s.io/api/apps/v1"
	"k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	UNIDLER             = "unidler"
	UNIDLER_NS          = "default"
	IDLED_LABEL         = "mojanalytics.xyz/idled"
	IDLED_AT_ANNOTATION = "mojanalytics.xyz/idled-at"
)

type App struct {
	Host       string
	Config     *Config
	ingress    *v1beta1.Ingress
	deployment *v1.Deployment
}

func NewApp(host string, config *Config) *App {
	return &App{
		Host:   host,
		Config: config,
	}
}

func (a *App) Unidle() error {
	err := a.getIngress()
	if err != nil {
		return err
	}

	err = a.getDeployment()
	if err != nil {
		return err
	}

	err = a.setReplicas(1)
	if err != nil {
		return err
	}

	err = a.waitForDeployment()
	if err != nil {
		return err
	}

	err = a.enableIngress()
	if err != nil {
		return err
	}

	// err = a.removeIdledMetadata()
	// if err != nil {
	// 	return err
	// }

	return nil
}

func (a *App) getIngress() error {
	opts := metav1.ListOptions{
		FieldSelector: fmt.Sprintf("metadata.name!=%s", UNIDLER),
	}

	// NOTE: can't filter by spec.rules[0].host
	list, err := a.Config.K8s.ExtensionsV1beta1().Ingresses("").List(opts)
	if err != nil {
		return err
	}

	for _, ing := range list.Items {
		if ing.Spec.Rules[0].Host == a.Host {
			a.Config.Logger.Printf("Ingress found: '%s' (ns: '%s')\n", ing.Name, ing.Namespace)
			a.ingress = &ing
			return nil
		}
	}

	return fmt.Errorf("Can't fine ingress for host '%s'", a.Host)
}

// Get the deployment for the app to unidle
//
// This is the deployment with same name/namespace as ingress
func (a *App) getDeployment() error {
	deployment, err := a.Config.K8s.Apps().Deployments(a.ingress.Namespace).Get(a.ingress.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("Can't find deployment '%s' in namespace '%s': %s", a.ingress.Name, a.ingress.Namespace, err)
	}

	a.Config.Logger.Printf("Deployment found: '%s' (ns: '%s')\n", deployment.Name, deployment.Namespace)
	a.deployment = deployment
	return nil
}

func (a *App) setReplicas(replicas int) error {
	a.Config.Logger.Printf("Deployment '%s' (ns: '%s') had %d replicas", a.deployment.Name, a.deployment.Namespace, *a.deployment.Spec.Replicas)

	patch := fmt.Sprintf("{\"spec\":{\"replicas\": %d}}", replicas)
	deploymentPatched, err := a.Config.K8s.Apps().Deployments(a.deployment.Namespace).Patch(a.deployment.Name, types.MergePatchType, []byte(patch))
	if err != nil {
		return fmt.Errorf("PATCH to set replicas to %d failed on deployment '%s' (ns: '%s'): %s", replicas, a.deployment.Name, a.deployment.Namespace, err)
	}

	a.deployment = deploymentPatched
	a.Config.Logger.Printf("Deployment '%s' (ns: '%s') has now %d replicas", a.deployment.Name, a.deployment.Namespace, *a.deployment.Spec.Replicas)

	return nil
}

func (a *App) enableIngress() error {
	// TODO
	return nil
}

func (a *App) waitForDeployment() error {
	opts := metav1.ListOptions{
		FieldSelector: fmt.Sprintf("metadata.name==%s", a.deployment.Name),
	}
	watchRes, err := a.Config.K8s.Apps().Deployments(a.deployment.Namespace).Watch(opts)
	if err != nil {
		return fmt.Errorf("Error while trying to watch deployment '%s' (ns: %s): %s", a.deployment.Name, a.deployment.Namespace, err)
	}

	for event := range watchRes.ResultChan() {
		deployment, ok := event.Object.(*v1.Deployment)
		if !ok {
			return fmt.Errorf("Watch event returned an unexpected object type: Expected *v1.Deployment Got: %+v", event.Object)
		}

		if deployment.Status.AvailableReplicas > 0 {
			a.Config.Logger.Printf("Deployment '%s' (ns: '%s') has now available replicas", deployment.Name, deployment.Namespace)
			break
		}
	}

	return nil
}

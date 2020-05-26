package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	binDir := os.ExpandEnv("${HOME}/.helm-wrapper/bin")

	if err := dirs(binDir); err != nil {
		log.Fatalln(err)
	}

	v := "v2.16.7"
	ok, err := checkLocal(v, binDir)
	if err != nil {
		log.Fatalln(err)
	}

	if !ok {
		if err := download(v); err != nil {
			log.Fatalln(err)
		}

		if err := unTarZip(v, binDir); err != nil {
			log.Fatalln(err)
		}
	}

	server, err := serverVersion(v, binDir)
	if err != nil {
		log.Fatalln(err)
	}

	if v != server {
		ok, err := checkLocal(server, binDir)
		if err != nil {
			log.Fatalln(err)
		}

		if !ok {
			if err := download(server); err != nil {
				log.Fatalln(err)
			}

			if err := unTarZip(server, binDir); err != nil {
				log.Fatalln(err)
			}
		}
	}

	cmd := exec.Command(fmt.Sprintf("%s/helm-%v", binDir, server), os.Args[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprint(os.Stdout, string(out), err)
		os.Exit(1)
	}

	fmt.Fprint(os.Stdout, string(out))
}

func dirs(path string) error {
	_, err := os.Stat(path)
	if err == nil {
		return nil
	}

	if !os.IsNotExist(err) {
		return err
	}

	if err := os.MkdirAll(path, 0755); err != nil {
		return err
	}

	return nil
}

func checkLocal(v, path string) (bool, error) {
	_, err := os.Stat(fmt.Sprintf("%s/helm-%v", path, v))
	if err == nil {
		return true, nil
	}

	if !os.IsNotExist(err) {
		return false, err
	}

	return false, nil
}

func download(v string) error {
	c := http.Client{
		Timeout: time.Second * 120,
	}

	url := fmt.Sprintf("https://get.helm.sh/helm-%s-%s-%s.tar.gz", v, runtime.GOOS, runtime.GOARCH)
	resp, err := c.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("couldn't download helm %s: %q", v, resp.Status)
	}

	outFile, err := os.Create(fmt.Sprintf("%s/helm-%s.tar.gz", os.TempDir(), v))
	if err != nil {
		return err
	}

	if _, err := io.Copy(outFile, resp.Body); err != nil {
		return err
	}

	return nil
}

func unTarZip(v, dir string) error {
	f, err := os.Open(fmt.Sprintf("%s/helm-%s.tar.gz", os.TempDir(), v))
	if err != nil {
		return err
	}
	defer os.Remove(fmt.Sprintf("%s/helm-%s.tar.gz", os.TempDir(), v))
	defer f.Close()

	archive, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer archive.Close()

	tr := tar.NewReader(archive)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		path := header.Name

		if header.Typeflag != tar.TypeReg {
			continue
		}

		if path != fmt.Sprintf("%s-%s/helm", runtime.GOOS, runtime.GOARCH) {
			continue
		}

		ofile, err := os.Create(fmt.Sprintf("%s/helm-%v", dir, v))
		if err != nil {
			return err
		}
		defer ofile.Close()

		if _, err := io.Copy(ofile, tr); err != nil {
			return err
		}

		if err := os.Chmod(fmt.Sprintf("%s/helm-%v", dir, v), 0755); err != nil {
			return err
		}
	}

	return nil
}

func serverVersion(v, dir string) (string, error) {
	ok, err := checkTiller()
	if err != nil {
		return "", err
	}

	if !ok {
		return v, nil
	}

	out, err := exec.Command(fmt.Sprintf("%s/helm-%v", dir, v), "version", "--server", "--template", "{{.Server.SemVer}}").CombinedOutput()
	if err != nil {
		return "", err
	}

	return string(out), nil
}

func checkTiller() (bool, error) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}

	kubeconfig := filepath.Join(homedir, ".kube", "config")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return false, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return false, err
	}

	listOptions := metav1.ListOptions{
		LabelSelector: "app=helm,name=tiller",
	}

	pods, err := clientset.CoreV1().Pods("kube-system").List(context.TODO(), listOptions)
	if err != nil {
		return false, err
	}

	if len(pods.Items) == 0 {
		return false, nil
	}

	return true, nil
}

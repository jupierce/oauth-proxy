package e2e

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	mathrand "math/rand"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func TestOAuthProxyE2E(t *testing.T) {
	testCtx := context.Background()

	ns := os.Getenv("TEST_NAMESPACE")
	oauthProxyTests := map[string]struct {
		oauthProxyArgs []string
		expectedErr    string
		accessSubPath  string
		pageResult     string
		backendEnvs    []string
		bypass         bool
	}{
		"basic": {
			oauthProxyArgs: []string{
				"--https-address=:8443",
				"--provider=openshift",
				"--openshift-service-account=proxy",
				"--upstream=http://localhost:8080",
				"--tls-cert=/etc/tls/private/tls.crt",
				"--tls-key=/etc/tls/private/tls.key",
				"--tls-client-ca=/etc/tls/private/ca.crt",
				"--skip-provider-button",
				"--cookie-secret=SECRET",
			},
			backendEnvs: []string{},
			expectedErr: "",
			pageResult:  "Hello OpenShift!\n",
		},
		// Tests a scope that is not valid for SA OAuth client use
		"scope-full": {
			oauthProxyArgs: []string{
				"--https-address=:8443",
				"--provider=openshift",
				"--openshift-service-account=proxy",
				"--upstream=http://localhost:8080",
				"--tls-cert=/etc/tls/private/tls.crt",
				"--tls-key=/etc/tls/private/tls.key",
				"--tls-client-ca=/etc/tls/private/ca.crt",
				"--cookie-secret=SECRET",
				"--skip-provider-button",
				"--scope=user:full",
			},
			backendEnvs: []string{},
			expectedErr: "403 Permission Denied",
			pageResult:  "Hello OpenShift!\n",
		},
		"sar-ok": {
			oauthProxyArgs: []string{
				"--https-address=:8443",
				"--provider=openshift",
				"--openshift-service-account=proxy",
				"--upstream=http://localhost:8080",
				"--tls-cert=/etc/tls/private/tls.crt",
				"--tls-key=/etc/tls/private/tls.key",
				"--tls-client-ca=/etc/tls/private/ca.crt",
				"--cookie-secret=SECRET",
				"--skip-provider-button",
				`--openshift-sar={"namespace":"` + ns + `","resource":"services","verb":"list"}`,
			},
			backendEnvs: []string{},
			expectedErr: "",
			pageResult:  "Hello OpenShift!\n",
		},
		"sar-fail": {
			oauthProxyArgs: []string{
				"--https-address=:8443",
				"--provider=openshift",
				"--openshift-service-account=proxy",
				"--upstream=http://localhost:8080",
				"--tls-cert=/etc/tls/private/tls.crt",
				"--tls-key=/etc/tls/private/tls.key",
				"--tls-client-ca=/etc/tls/private/ca.crt",
				"--cookie-secret=SECRET",
				"--skip-provider-button",
				`--openshift-sar={"namespace":"other","resource":"services","verb":"list"}`,
			},
			backendEnvs: []string{},
			expectedErr: "did not reach upstream site",
			pageResult:  "Hello OpenShift!\n",
		},
		"sar-multi-ok": {
			oauthProxyArgs: []string{
				"--https-address=:8443",
				"--provider=openshift",
				"--openshift-service-account=proxy",
				"--upstream=http://localhost:8080",
				"--tls-cert=/etc/tls/private/tls.crt",
				"--tls-key=/etc/tls/private/tls.key",
				"--tls-client-ca=/etc/tls/private/ca.crt",
				"--cookie-secret=SECRET",
				"--skip-provider-button",
				`--openshift-sar=[{"namespace":"` + ns + `","resource":"services","verb":"list"}, {"namespace":"` + ns + `","resource":"routes","verb":"list"}]`,
			},
			backendEnvs: []string{},
			expectedErr: "",
			pageResult:  "Hello OpenShift!\n",
		},
		"sar-multi-fail": {
			oauthProxyArgs: []string{
				"--https-address=:8443",
				"--provider=openshift",
				"--openshift-service-account=proxy",
				"--upstream=http://localhost:8080",
				"--tls-cert=/etc/tls/private/tls.crt",
				"--tls-key=/etc/tls/private/tls.key",
				"--tls-client-ca=/etc/tls/private/ca.crt",
				"--cookie-secret=SECRET",
				"--skip-provider-button",
				`--openshift-sar=[{"namespace":"` + ns + `","resource":"services","verb":"list"}, {"namespace":"other","resource":"pods","verb":"list"}]`,
			},
			backendEnvs: []string{},
			expectedErr: "did not reach upstream site",
			pageResult:  "Hello OpenShift!\n",
		},
		"skip-auth-regex-bypass-foo": {
			oauthProxyArgs: []string{
				"--https-address=:8443",
				"--provider=openshift",
				"--openshift-service-account=proxy",
				"--upstream=http://localhost:8080",
				"--tls-cert=/etc/tls/private/tls.crt",
				"--tls-key=/etc/tls/private/tls.key",
				"--tls-client-ca=/etc/tls/private/ca.crt",
				"--cookie-secret=SECRET",
				"--skip-provider-button",
				`--skip-auth-regex=^/foo`,
			},
			backendEnvs:   []string{"HELLO_SUBPATHS=/foo,/bar"},
			accessSubPath: "/foo",
			expectedErr:   "",
			pageResult:    "Hello OpenShift! /foo\n",
			bypass:        true,
		},
		"skip-auth-regex-protect-bar": {
			oauthProxyArgs: []string{
				"--https-address=:8443",
				"--provider=openshift",
				"--openshift-service-account=proxy",
				"--upstream=http://localhost:8080",
				"--tls-cert=/etc/tls/private/tls.crt",
				"--tls-key=/etc/tls/private/tls.key",
				"--tls-client-ca=/etc/tls/private/ca.crt",
				"--cookie-secret=SECRET",
				"--skip-provider-button",
				`--skip-auth-regex=^/foo`,
			},
			backendEnvs:   []string{"HELLO_SUBPATHS=/foo,/bar"},
			accessSubPath: "/bar",
			expectedErr:   "",
			pageResult:    "Hello OpenShift! /bar\n",
		},
		// test --bypass-auth-for (alias for --skip-auth-regex); expect to bypass auth for /foo
		"bypass-auth-foo": {
			oauthProxyArgs: []string{
				"--https-address=:8443",
				"--provider=openshift",
				"--openshift-service-account=proxy",
				"--upstream=http://localhost:8080",
				"--tls-cert=/etc/tls/private/tls.crt",
				"--tls-key=/etc/tls/private/tls.key",
				"--tls-client-ca=/etc/tls/private/ca.crt",
				"--cookie-secret=SECRET",
				"--skip-provider-button",
				`--bypass-auth-for=^/foo`,
			},
			backendEnvs:   []string{"HELLO_SUBPATHS=/foo,/bar"},
			accessSubPath: "/foo",
			expectedErr:   "",
			pageResult:    "Hello OpenShift! /foo\n",
			bypass:        true,
		},
		// test --bypass-auth-except-for; expect to auth /foo
		"bypass-auth-except-try-protected": {
			oauthProxyArgs: []string{
				"--https-address=:8443",
				"--provider=openshift",
				"--openshift-service-account=proxy",
				"--upstream=http://localhost:8080",
				"--tls-cert=/etc/tls/private/tls.crt",
				"--tls-key=/etc/tls/private/tls.key",
				"--tls-client-ca=/etc/tls/private/ca.crt",
				"--cookie-secret=SECRET",
				"--skip-provider-button",
				`--bypass-auth-except-for=^/foo`,
			},
			backendEnvs:   []string{"HELLO_SUBPATHS=/foo,/bar"},
			accessSubPath: "/foo",
			expectedErr:   "",
			pageResult:    "Hello OpenShift! /foo\n",
		},
		// test --bypass-auth-except-for; expect to bypass auth for paths other than /foo
		"bypass-auth-except-try-bypassed": {
			oauthProxyArgs: []string{
				"--https-address=:8443",
				"--provider=openshift",
				"--openshift-service-account=proxy",
				"--upstream=http://localhost:8080",
				"--tls-cert=/etc/tls/private/tls.crt",
				"--tls-key=/etc/tls/private/tls.key",
				"--tls-client-ca=/etc/tls/private/ca.crt",
				"--cookie-secret=SECRET",
				"--skip-provider-button",
				`--bypass-auth-except-for=^/foo`,
			},
			backendEnvs:   []string{"HELLO_SUBPATHS=/foo,/bar"},
			accessSubPath: "/bar",
			expectedErr:   "",
			pageResult:    "Hello OpenShift! /bar\n",
			bypass:        true,
		},
		// --upstream-ca set with the CA for the backend site's certificate
		"upstream-ca": {
			oauthProxyArgs: []string{
				"--https-address=:8443",
				"--provider=openshift",
				"--openshift-service-account=proxy",
				"--upstream=https://localhost:8080",
				"--upstream-ca=/etc/tls/private/upstreamca.crt",
				"--tls-cert=/etc/tls/private/tls.crt",
				"--tls-key=/etc/tls/private/tls.key",
				"--tls-client-ca=/etc/tls/private/ca.crt",
				"--skip-provider-button",
				"--cookie-secret=SECRET",
			},
			backendEnvs: []string{"HELLO_TLS_CERT=/etc/tls/private/upstream.crt", "HELLO_TLS_KEY=/etc/tls/private/upstream.key"},
			expectedErr: "",
			pageResult:  "Hello OpenShift!\n",
		},
		// --upstream-ca set multiple times, with one matching CA
		"upstream-ca-multi": {
			oauthProxyArgs: []string{
				"--https-address=:8443",
				"--provider=openshift",
				"--openshift-service-account=proxy",
				"--upstream=https://localhost:8080",
				"--upstream-ca=/etc/tls/private/upstreamca.crt",
				"--upstream-ca=/etc/tls/private/ca.crt",
				"--tls-cert=/etc/tls/private/tls.crt",
				"--tls-key=/etc/tls/private/tls.key",
				"--tls-client-ca=/etc/tls/private/ca.crt",
				"--skip-provider-button",
				"--cookie-secret=SECRET",
			},
			backendEnvs: []string{"HELLO_TLS_CERT=/etc/tls/private/upstream.crt", "HELLO_TLS_KEY=/etc/tls/private/upstream.key"},
			expectedErr: "",
			pageResult:  "Hello OpenShift!\n",
		},
		// no --upstream-ca set, so there's no valid TLS connection between proxy and upstream
		"upstream-ca-missing": {
			oauthProxyArgs: []string{
				"--https-address=:8443",
				"--provider=openshift",
				"--openshift-service-account=proxy",
				"--upstream=https://localhost:8080",
				"--tls-cert=/etc/tls/private/tls.crt",
				"--tls-key=/etc/tls/private/tls.key",
				"--tls-client-ca=/etc/tls/private/ca.crt",
				"--skip-provider-button",
				"--cookie-secret=SECRET",
			},
			backendEnvs: []string{"HELLO_TLS_CERT=/etc/tls/private/upstream.crt", "HELLO_TLS_KEY=/etc/tls/private/upstream.key"},
			expectedErr: "did not reach upstream site",
			pageResult:  "Hello OpenShift!\n",
		},
	}

	image := os.Getenv("TEST_IMAGE")
	backendImage := os.Getenv("HELLO_IMAGE")

	mathrand.Seed(time.Now().UTC().UnixNano())
	kubeConfig, err := loadConfig(os.Getenv("KUBECONFIG"), os.Getenv("KUBECONTEXT"))
	if err != nil {
		t.Fatalf("error loading kubeconfig: '%s', ctx: '%s', err: %s", os.Getenv("KUBECONFIG"), os.Getenv("KUBECONTEXT"), err)
	}
	kubeClientSet, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		t.Fatalf("error generating kube client: %s", err)
	}

	t.Logf("test image: %s, test namespace: %s", image, ns)

	for tcName, tc := range oauthProxyTests {
		runOnly := os.Getenv("TEST")
		if len(runOnly) > 0 && runOnly != tcName {
			continue
		}
		t.Run(fmt.Sprintf("setting up e2e tests %s", tcName), func(t *testing.T) {
			_, err := kubeClientSet.CoreV1().ServiceAccounts(ns).Create(testCtx, newOAuthProxySA(), metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("setup: error creating SA: %s", err)
			}

			err = newOAuthProxyRoute(ns)
			if err != nil {
				t.Fatalf("setup: error creating route: %s", err)
			}

			// Find the exposed route hostname that we will be doing client actions against
			proxyRouteHost, err := getRouteHost("proxy-route", ns)
			if err != nil {
				t.Fatalf("setup: error finding route host: %s", err)
			}

			// Create the TLS certificate set for the client and service (with the route hostname attributes)
			caPem, serviceCert, serviceKey, err := createCAandCertSet(proxyRouteHost)
			if err != nil {
				t.Fatalf("setup: error creating TLS certs: %s", err)
			}

			// Create the TLS certificate set for the proxy backend (-upstream-ca) and the upstream site
			upstreamCA, upstreamCert, upstreamKey, err := createCAandCertSet("localhost")
			if err != nil {
				t.Fatalf("setup: error creating upstream TLS certs: %s", err)
			}

			_, err = kubeClientSet.CoreV1().Services(ns).Create(testCtx, newOAuthProxyService(), metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("setup: error creating service: %s", err)
			}

			// configMap provides oauth-proxy with the certificates we created above
			_, err = kubeClientSet.CoreV1().ConfigMaps(ns).Create(testCtx, newOAuthProxyConfigMap(ns, caPem, serviceCert, serviceKey, upstreamCA, upstreamCert, upstreamKey), metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("setup: error creating certificate configMap: %s", err)
			}

			oauthProxyPod, err := kubeClientSet.CoreV1().Pods(ns).Create(testCtx, newOAuthProxyPod(image, backendImage, tc.oauthProxyArgs, tc.backendEnvs), metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("setup: error creating oauth-proxy pod with image '%s' and args '%v': %s", image, tc.oauthProxyArgs, err)
			}

			err = waitForPodRunningInNamespace(kubeClientSet, oauthProxyPod)
			if err != nil {
				t.Fatalf("setup: error waiting for pod to run: %s", err)
			}

			// Find the service CA for the client trust store
			secrets, err := kubeClientSet.CoreV1().Secrets(ns).List(testCtx, metav1.ListOptions{})
			if err != nil {
				t.Fatalf("setup: error listing secrets: %s", err)
			}

			var openshiftPemCA []byte
			for _, s := range secrets.Items {
				cert, ok := s.Data["ca.crt"]
				if !ok {
					continue
				}
				openshiftPemCA = cert
				break
			}
			if openshiftPemCA == nil {
				t.Fatalf("setup: could not find openshift CA from secrets")
			}

			host := "https://" + proxyRouteHost + "/oauth/start"
			// Wait for the route, we get an EOF if we move along too fast
			err = waitUntilRouteIsReady([][]byte{caPem, openshiftPemCA}, host)
			if err != nil {
				t.Fatalf("setup: error waiting for route availability: %s", err)
			}

			user := randLogin()
			// For SAR tests the random user needs the admin role for this namespace.
			out, err := execCmd("oc", []string{"adm", "policy", "add-role-to-user", "admin", user, "-n", ns, "--rolebinding-name", "sar-" + user}, "")
			if err != nil {
				t.Fatalf("setup: error setting test user role: %s", err)
			}
			t.Logf("%s", out)

			defer func() {
				if os.Getenv("DEBUG_TEST") == tcName {
					t.Fatalf("skipping cleanup step for test '%s' and stopping on command", tcName)
				}
				t.Logf("cleaning up test %s", tcName)
				kubeClientSet.CoreV1().Pods(ns).Delete(testCtx, "proxy", metav1.DeleteOptions{})
				kubeClientSet.CoreV1().Services(ns).Delete(testCtx, "proxy", metav1.DeleteOptions{})
				deleteTestRoute("proxy-route", ns)
				kubeClientSet.CoreV1().ConfigMaps(ns).Delete(testCtx, "proxy-certs", metav1.DeleteOptions{})
				kubeClientSet.CoreV1().ServiceAccounts(ns).Delete(testCtx, "proxy", metav1.DeleteOptions{})
				waitForPodDeletion(kubeClientSet, oauthProxyPod.Name, ns)
				execCmd("oc", []string{"adm", "policy", "remove-role-from-user", "admin", user, "-n", ns}, "")
			}()

			waitForHealthzCheck([][]byte{caPem, openshiftPemCA}, "https://"+proxyRouteHost)

			check3DESDisabled(t, [][]byte{caPem, openshiftPemCA}, "https://"+proxyRouteHost)

			err = confirmOAuthFlow(proxyRouteHost, tc.accessSubPath, [][]byte{caPem, openshiftPemCA}, user, tc.pageResult, tc.expectedErr, tc.bypass)

			if err == nil && len(tc.expectedErr) > 0 {
				t.Errorf("expected error '%s', but test passed", tc.expectedErr)
			}

			if err != nil {
				if len(tc.expectedErr) > 0 {
					if tc.expectedErr != err.Error() {
						t.Errorf("expected error '%s', got '%s'", tc.expectedErr, err)
					}
				} else {
					t.Errorf("test failed with '%s'", err)
				}
			}
		})
	}
}

func submitOAuthForm(client *http.Client, response *http.Response, user, expectedErr string) (*http.Response, error) {
	responseBytes, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	responseBuffer := bytes.NewBuffer(responseBytes)

	body, err := html.Parse(responseBuffer)
	if err != nil {
		return nil, err
	}

	forms := getElementsByTagName(body, "form")
	if len(forms) != 1 {
		errMsg := "expected OpenShift form"
		// Return the expected error if it's found amongst the text elements
		if expectedErr != "" {
			checkBuffer := bytes.NewBuffer(responseBytes)
			parsed, err := html.Parse(checkBuffer)
			if err != nil {
				return nil, err
			}

			textNodes := getTextNodes(parsed)
			for i := range textNodes {
				if textNodes[i].Data == expectedErr {
					errMsg = expectedErr
				}
			}
		}
		return nil, fmt.Errorf(errMsg)
	}

	formReq, err := newRequestFromForm(forms[0], response.Request.URL, user)
	if err != nil {
		return nil, err
	}

	postResp, err := client.Do(formReq)
	if err != nil {
		return nil, err
	}

	return postResp, nil
}

func confirmOAuthFlow(host, subPath string, cas [][]byte, user, expectedPageResult, expectedErr string, expectedBypass bool) error {
	// Set up the client cert store
	client, err := newHTTPSClient(cas)
	if err != nil {
		return err
	}

	startUrl := "https://" + host + subPath
	resp, err := getResponse(startUrl, client)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if !expectedBypass {
		// OpenShift login
		loginResp, err := submitOAuthForm(client, resp, user, expectedErr)
		if err != nil {
			return err
		}
		defer loginResp.Body.Close()

		// authorization grant form
		grantResp, err := submitOAuthForm(client, loginResp, user, expectedErr)
		if err != nil {
			return err
		}

		defer grantResp.Body.Close()
		resp = grantResp
	}

	accessRespBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	if string(accessRespBody) != expectedPageResult {
		return fmt.Errorf("did not reach upstream site")
	}

	return nil
}

func check3DESDisabled(t *testing.T, cas [][]byte, url string) {
	pool := x509.NewCertPool()
	for _, ca := range cas {
		if !pool.AppendCertsFromPEM(ca) {
			t.Fatal("error loading CA for client config")
		}
	}

	jar, _ := cookiejar.New(nil)
	tr := &http.Transport{
		MaxIdleConns:    10,
		IdleConnTimeout: 30 * time.Second,
		TLSClientConfig: &tls.Config{
			RootCAs: pool,
			CipherSuites: []uint16{
				tls.TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA,
				tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA,
			},
		},
	}

	client := &http.Client{Transport: tr, Jar: jar}
	resp, err := getResponse(url, client)

	if err == nil {
		resp.Body.Close()
		t.Fatal("expected to fail with weak ciphers")
	}

	if !strings.Contains(err.Error(), "handshake failure") {
		t.Fatalf("expected TLS handshake error with weak ciphers, got: %v", err)
	}
}

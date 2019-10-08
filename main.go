package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/transport/spdy"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
)

// END_OF_TRANSMISSION code
const END_OF_TRANSMISSION = "\u0004"

// Applicable SSH Request types for Port Forwarding - RFC 4254 7.X
const (
	DirectForwardRequest       = "direct-tcpip"         // RFC 4254 7.2
	RemoteForwardRequest       = "tcpip-forward"        // RFC 4254 7.1
	ForwardedTCPReturnRequest  = "forwarded-tcpip"      // RFC 4254 7.2
	CancelRemoteForwardRequest = "cancel-tcpip-forward" // RFC 4254 7.1
)

// PortForwardProtocolV1Name is required to forward ports to containers
const PortForwardProtocolV1Name = "portforward.k8s.io"

func main() {

	ssh.Handle(func(sess ssh.Session) {
		sc := make(chan ssh.Signal)
		sess.Signals(sc)
		go func() {
			for signal := range sc {
				log.Printf("Signal: %v\n", signal)
			}
		}()

		_, _, isTty := sess.Pty()
		name := fmt.Sprintf("remote-vsc-%s", sess.User())

		// we need a logger per session
		c, err := NewCluster()
		if err != nil {
			log.Println(err)
			sess.Exit(1)
			return
		}

		go func() {
			for msg := range c.log {
				log.Printf("%s\t%s\t%s", sess.RemoteAddr(), sess.User(), msg)
				io.WriteString(sess, msg)
			}
		}()

		if _, err := c.checkExistingPod(name); err != nil {
			c.pod(sess.User(), name)
			err = c.waitForPod(name)
			if err != nil {
				c.log <- fmt.Sprintf("%s", err.Error())
				sess.Exit(1)
				return
			}
		} else {
			c.log <- fmt.Sprintf("We already have an exiting pod for user %q\n", sess.User())
		}

		err = c.getTerminal(name, sess, isTty)
		if err != nil {
			c.log <- fmt.Sprintf(err.Error())
			sess.Exit(1)
			return
		}

		sess.Exit(0)
	})

	log.Println("Starting ssh server on port 2222...")

	s := &ssh.Server{
		Addr: "0.0.0.0:2222",
		PublicKeyHandler: func(ctx ssh.Context, key ssh.PublicKey) bool {
			keys := os.Getenv("SSH_KEYS")
			if len(keys) == 0 {
				log.Println("No SSH keys loaded")
				return false
			}

			scanner := bufio.NewScanner(strings.NewReader(keys))
			for scanner.Scan() {
				trustedKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(scanner.Text()))
				if err != nil {
					log.Println("Failed to parse configured public key:", scanner.Text())
				}

				if ssh.KeysEqual(key, trustedKey) {
					return true
				}
			}

			log.Println("Unknown SSH key")
			return false
		},
		ReversePortForwardingCallback: ssh.ReversePortForwardingCallback(func(ctx ssh.Context, host string, port uint32) bool {
			log.Printf("Accepted binding for host %q on port %d\n", host, port)
			return true
		}),
		LocalPortForwardingCallback: ssh.LocalPortForwardingCallback(func(ctx ssh.Context, dhost string, dport uint32) bool {
			log.Printf("Accepted port forwarding for host %q on port %d\n", dhost, dport)
			return true
		}),
		ChannelHandlers: map[string]ssh.ChannelHandler{
			"session":                  ssh.DefaultSessionHandler,
			DirectForwardRequest:       channelHandler,
			RemoteForwardRequest:       channelHandler,
			ForwardedTCPReturnRequest:  channelHandler,
			CancelRemoteForwardRequest: channelHandler,
		},
	}

	// TODO: Use configured host key
	//s.AddHostKey(hostKeySigner)

	log.Fatal(s.ListenAndServe())
}

// direct-tcpip data struct as specified in RFC4254, Section 7.2
type localForwardChannelData struct {
	DestAddr string
	DestPort uint32

	OriginAddr string
	OriginPort uint32
}

func channelHandler(srv *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, ctx ssh.Context) {
	d := localForwardChannelData{}
	if err := gossh.Unmarshal(newChan.ExtraData(), &d); err != nil {
		newChan.Reject(gossh.ConnectionFailed, "error parsing forward data: "+err.Error())
		return
	}

	fmt.Printf("localForwardChannelData: %#v\n\n", d)

	if srv.LocalPortForwardingCallback == nil || !srv.LocalPortForwardingCallback(ctx, d.DestAddr, d.DestPort) {
		newChan.Reject(gossh.Prohibited, "port forwarding is disabled")
		return
	}

	name := fmt.Sprintf("remote-vsc-%s", ctx.User())

	// we need a logger per session
	c, err := NewCluster()
	if err != nil {
		log.Println(err)
		//sess.Exit(1)
		return
	}

	req := c.client.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(os.Getenv("NAMESPACE")).
		Name(name).
		SubResource("portforward")

	transport, upgrader, err := spdy.RoundTripperFor(c.config)
	if err != nil {
		log.Println(err)
		//sess.Exit(1)
		return
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", req.URL())
	if err != nil {
		log.Println(err)
		//sess.Exit(1)
		return
	}

	stream, _, err := dialer.Dial(PortForwardProtocolV1Name)
	if err != nil {
		log.Printf("error upgrading connection: %s", err)
		//sess.Exit(1)
		return
	}
	defer stream.Close()

	// create error stream
	headers := http.Header{}
	headers.Set(apiv1.StreamType, apiv1.StreamTypeError)
	headers.Set(apiv1.PortHeader, fmt.Sprintf("%d", d.DestPort))
	headers.Set(apiv1.PortForwardRequestIDHeader, ctx.SessionID())
	errorStream, err := stream.CreateStream(headers)
	if err != nil {
		runtime.HandleError(fmt.Errorf("error creating error stream for port %d -> %d: %v", d.OriginPort, d.DestPort, err))
		return
	}
	// we're not writing to this stream
	errorStream.Close()

	errorChan := make(chan error)
	go func() {
		message, err := ioutil.ReadAll(errorStream)
		switch {
		case err != nil:
			errorChan <- fmt.Errorf("error reading from error stream for port %d -> %d: %v", d.OriginPort, d.DestPort, err)
		case len(message) > 0:
			errorChan <- fmt.Errorf("an error occurred forwarding %d -> %d: %v", d.OriginPort, d.DestPort, string(message))
		}
		//close(errorChan)
	}()

	// create data stream
	headers.Set(apiv1.StreamType, apiv1.StreamTypeData)
	dataStream, err := stream.CreateStream(headers)
	if err != nil {
		runtime.HandleError(fmt.Errorf("error creating forwarding stream for port %d -> %d: %v", d.OriginPort, d.DestPort, err))
		return
	}

	localError := make(chan struct{})
	remoteDone := make(chan struct{})

	// accept ssh channel
	ch, reqs, err := newChan.Accept()
	if err != nil {
		log.Printf("failed to accept channel: %s", err)
		//sess.Exit(1)
		return
	}
	go gossh.DiscardRequests(reqs)

	go func() {
		// Copy from the remote side to the local port.
		if _, err := io.Copy(ch, dataStream); err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
			runtime.HandleError(fmt.Errorf("error copying from remote stream to local connection: %v", err))
		}

		// inform the select below that the remote copy is done
		close(remoteDone)
	}()

	go func() {
		// inform server we're not sending any more data after copy unblocks
		defer dataStream.Close()

		// Copy from the local port to the remote side.
		if _, err := io.Copy(dataStream, ch); err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
			runtime.HandleError(fmt.Errorf("error copying from local connection to remote stream: %v", err))
			// break out of the select below without waiting for the other copy to finish
			close(localError)
		}
	}()

	// wait for either a local->remote error or for copying from remote->local to finish
	select {
	case <-remoteDone:
	case <-localError:
	}

	// always expect something on errorChan (it may be nil)
	err = <-errorChan
	if err != nil {
		log.Println("errorChan", err)
		runtime.HandleError(err)
	}
}

// Cluster configuration
type Cluster struct {
	config *rest.Config
	client *kubernetes.Clientset
	log    chan string
}

// NewCluster configuration
func NewCluster() (*Cluster, error) {
	var err error

	c := &Cluster{
		log: make(chan string),
	}
	c.config, err = clientcmd.RESTConfigFromKubeConfig([]byte(os.Getenv("KUBE_CONFIG")))
	if err != nil {
		return nil, err
	}
	c.client, err = kubernetes.NewForConfig(c.config)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Cluster) getTerminal(name string, sess ssh.Session, isTty bool) error {
	c.log <- fmt.Sprint("Trying to connect container\n")

	req := c.client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(name).
		Namespace(os.Getenv("NAMESPACE")).
		SubResource("exec")

	req.VersionedParams(&apiv1.PodExecOptions{
		Container: "remote-vsc-container",
		Command:   []string{"bash"},
		Stdin:     true,
		Stdout:    true,
		Stderr:    true,
		TTY:       isTty,
	}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(c.config, "POST", req.URL())
	if err != nil {
		return err
	}

	_, w, _ := sess.Pty()

	c.log <- fmt.Sprint("Starting stream\n")
	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:             sess,
		Stdout:            sess,
		Stderr:            sess.Stderr(),
		TerminalSizeQueue: TerminalSizeQueue{w},
		Tty:               true,
	})
	if err != nil {
		return err
	}

	c.log <- fmt.Sprint("Finished stream\n")

	return nil
}

func (c *Cluster) checkExistingPod(name string) (*apiv1.Pod, error) {
	podsClient := c.client.CoreV1().Pods(os.Getenv("NAMESPACE"))
	pod, err := podsClient.Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	if pod.Status.Phase == apiv1.PodFailed {
		c.log <- fmt.Sprintln(pod.Status.Message)
		for _, cond := range pod.Status.Conditions {
			c.log <- fmt.Sprintf("\t%s\n", cond.Message)
		}

		c.log <- fmt.Sprintln("Deleting pod")

		err = podsClient.Delete(name, nil)
		if err != nil {
			return nil, err
		}

		c.log <- fmt.Sprintln("Pod deleted")

		return nil, errors.New("pod deleted")
	}

	return pod, nil
}

func (c *Cluster) waitForPod(name string) error {
	return wait.PollImmediate(time.Second, 60*time.Second, func() (bool, error) {
		pod, err := c.checkExistingPod(name)
		if err != nil {
			return false, err
		}

		switch pod.Status.Phase {
		case apiv1.PodPending:
			c.log <- fmt.Sprintf("Waiting on pod to become available...\n")
			return false, nil
		case apiv1.PodFailed:
			return true, errors.New(pod.Status.Message)
		case apiv1.PodRunning:
			return true, nil
		}

		return true, errors.New("unknown pod status")
	})
}

func (c *Cluster) pod(user, name string) error {
	c.log <- fmt.Sprintf("Creating new pod for %q\n", user)

	podsClient := c.client.CoreV1().Pods(os.Getenv("NAMESPACE"))

	var privileged bool
	if os.Getenv("PRIVILEGED") == "true" {
		privileged = true
	}

	procMount := apiv1.DefaultProcMount
	if os.Getenv("PROCMOUNT") == "Unmasked" {
		procMount = apiv1.UnmaskedProcMount
	}

	image := "alpine"
	if os.Getenv("IMAGE") != "" {
		image = os.Getenv("IMAGE")
	}

	// TODO: Allow different configrations and images (image from session env?)
	pod := &apiv1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiv1.PodSpec{
			Containers: []apiv1.Container{
				{
					Name:  "remote-vsc-container",
					Image: image,
					SecurityContext: &apiv1.SecurityContext{
						Privileged: &privileged,
						ProcMount:  &procMount,
					},
					Command: []string{"/usr/bin/tail"},
					Args:    []string{"-f", "-"},
					Stdin:   true,
					TTY:     true,
				},
			},
		},
	}

	// Create Pod
	result, err := podsClient.Create(pod)
	if err != nil {
		return err
	}
	c.log <- fmt.Sprintf("Created pod %q\n", result.GetObjectMeta().GetName())
	return nil
}

// TerminalSizeQueue handler
type TerminalSizeQueue struct {
	w <-chan ssh.Window
}

// Next terminal size event
func (t TerminalSizeQueue) Next() *remotecommand.TerminalSize {
	w := <-t.w
	return &remotecommand.TerminalSize{
		Width:  uint16(w.Width),
		Height: uint16(w.Height),
	}
}

package pssh

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type Endpoint struct {
	ViaTunnel bool
	Ip        string
	Name      string
	UserName  string
	Password  string
	Port      string
	SshOut    io.Reader
	SshIn     io.WriteCloser
	Timeout   int
	Client    *ssh.Client
	Session   *ssh.Session
	Kind      string
}

func readBuffForString(whattoexpect []string, sshOut io.Reader, buffRead chan<- string, errChan chan error) {
	buf := make([]byte, 1000)
	waitingString := ""
	for {
		n, err := sshOut.Read(buf) //this reads the ssh terminal
		if err != nil && err != io.EOF {
			fmt.Println(err)
			break
		}
		if err == io.EOF || n == 0 {
			break
		}
		waitingString = strings.Join([]string{waitingString, string(buf[:n])}, "")
		if strings.Contains(waitingString, whattoexpect[0]) {
			buffRead <- waitingString
			break
		} else if len(whattoexpect) > 1 {
			if strings.Contains(waitingString, whattoexpect[1]) {
				errChan <- fmt.Errorf("wrong username/password")
				break
			}
		}
	}
}

func readBuff(whattoexpect []string, sshOut io.Reader, timeoutSeconds int) (string, error) {
	errChan := make(chan error)
	ch := make(chan string)
	go func(whattoexpect []string, sshOut io.Reader, errChan chan error) {
		buffRead := make(chan string)
		go readBuffForString(whattoexpect, sshOut, buffRead, errChan)
		select {
		case ret := <-buffRead:
			ch <- ret
		case <-time.After(time.Duration(timeoutSeconds) * time.Second):
		}
	}(whattoexpect, sshOut, errChan)
	select {
	case data := <-ch:
		return data, nil
	case err := <-errChan:
		return "", err
	case <-time.After(time.Duration(timeoutSeconds) * time.Second):
		return "", fmt.Errorf("timedout on: %v", whattoexpect)
	}
}

func writeBuff(command string, sshIn io.WriteCloser) (int, error) {
	returnCode, err := sshIn.Write([]byte(command + "\n"))
	return returnCode, err
}

//Connect connects to the specified server and opens a session (Filling the Client and Session fields in SshAgent struct).
func (s *Endpoint) Connect() error {
	if err := validateNode(s); err != nil {
		return err
	}

	var err error
	config := &ssh.ClientConfig{
		User: "cli",
		Auth: []ssh.AuthMethod{
			ssh.Password("cli"),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         time.Duration(s.Timeout) * time.Second,
	}

	if s.ViaTunnel {
		s.Client, err = ssh.Dial("tcp", fmt.Sprintf("%v:%v", "127.0.0.1", s.Port), config)
		if err != nil {
			return fmt.Errorf("%v:%v - %v", s.Ip, s.Port, err.Error())
		}
	} else {
		s.Client, err = ssh.Dial("tcp", fmt.Sprintf("%v:%v", s.Ip, s.Port), config)
		if err != nil {
			return fmt.Errorf("%v:%v - %v", s.Ip, s.Port, err.Error())
		}
	}
	if s.Kind == "1830PSS" {
		if err := s.cliLogin(); err != nil {
			return err
		}
	}
	return nil
}

//parseNeName extracts the actuall ne name and fills ne.Name variable.
func (s *Endpoint) parseNeName(lines []string) string {
	for _, l := range lines {
		if strings.Contains(l, "#") {
			trimedLine := strings.TrimSpace(l)
			if trimedLine[len(trimedLine)-1] == '#' {
				return trimedLine[:len(trimedLine)-1]
			}
		}
	}
	return s.Name
}

//CliLogin does the special login sequence needed to login to 1830PSS cli.
func (s *Endpoint) cliLogin() error {
	var err error
	modes := ssh.TerminalModes{
		// disable echoing.
		ssh.ECHO: 0,
		// input speed = 14.4kbaud.
		ssh.TTY_OP_ISPEED: 14400,
		// output speed = 14.4kbaud.
		ssh.TTY_OP_OSPEED: 14400,
	}
	s.Session, err = s.Client.NewSession()
	if err != nil {
		return fmt.Errorf("%v:%v - failure on Client.NewSession() - details: %v", s.Ip, s.Port, err.Error())
	}
	s.SshOut, err = s.Session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("%v:%v - failure on Session.StdoutPipe() - details: %v", s.Ip, s.Port, err.Error())
	}
	s.SshIn, err = s.Session.StdinPipe()
	if err != nil {
		return fmt.Errorf("%v:%v - failure on Session.StdinPipe() - details: %v", s.Ip, s.Port, err.Error())
	}
	if err := s.Session.RequestPty("xterm", 0, 200, modes); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on Session.RequestPty() - details: %v", s.Ip, s.Port, err.Error())
	}
	if err := s.Session.Shell(); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on Session.Shell() - details: %v", s.Ip, s.Port, err.Error())
	}
	if _, err := readBuff([]string{"Username:"}, s.SshOut, 6); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on readBuff(username) - details: %v", s.Ip, s.Port, err.Error())
	}
	if _, err := writeBuff(s.UserName, s.SshIn); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on writeBuff(s.UserName) - details: %v", s.Ip, s.Port, err.Error())
	}
	if _, err := readBuff([]string{"Password:"}, s.SshOut, 4); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on readBuff(Password) - details: %v", s.Ip, s.Port, err.Error())
	}
	if _, err := writeBuff(s.Password, s.SshIn); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on writeBuff(s.Password) - details: %v", s.Ip, s.Port, err.Error())
	}
	if _, err := readBuff([]string{"(Y/N)?", "authentication failed"}, s.SshOut, 4); err != nil {
		return fmt.Errorf("%v:%v - failure on readBuff(Y/N) - details: %v", s.Ip, s.Port, err.Error())
	}
	if _, err := writeBuff("y", s.SshIn); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on writeBuff(Y) - details: %v", s.Ip, s.Port, err.Error())
	}
	result, err := readBuff([]string{"#"}, s.SshOut, 4)
	if err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on readBuff(#) (INIT) - details: %v", s.Ip, s.Port, err.Error())
	}

	s.Name = s.parseNeName(strings.Split(result, "\r"))

	if _, err := writeBuff("paging status disable", s.SshIn); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on writeBuff(Page Status Disable) - details: %v", s.Ip, s.Port, err.Error())
	}
	if _, err := readBuff([]string{s.Name + "#"}, s.SshOut, 4); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on readBuff(#) (END) - details: %v", s.Ip, s.Port, err.Error())
	}
	return nil
}

//Run executes the given cli command on the opened session.
func (s *Endpoint) Run(env string, cmds ...string) (map[string]string, error) {
	result := map[string]string{}
	if s.Kind == "1830PSS" {
		prompt := []string{s.Name + "#"}
		if env == "gmre" {
			if err := s.gmreLogin(); err != nil {
				return nil, err
			}
			for c := range cmds {
				cmds[c] += "\r"
			}

			prompt = []string{"]#"}
		}

		for _, c := range cmds {
			if _, err := writeBuff(c, s.SshIn); err != nil {
				s.Session.Close()
				return nil, fmt.Errorf("%v:%v - failure on Run(%v) - details: %v", s.Ip, s.Port, c, err.Error())
			}

			data, err := readBuff(prompt, s.SshOut, 15)
			if err != nil {
				return nil, fmt.Errorf("%v:%v - failure on Run(%v) - readBuff(%v#) - details: %v", s.Ip, s.Port, s.Name, c, err.Error())
			}
			result[c] = data
		}

		if env == "gmre" {
			if err := s.gmreLogout(); err != nil {
				return nil, err
			}
		}
	} else if s.Kind == "Linux" {
		var err error
		for _, c := range cmds {
			s.Session, err = s.Client.NewSession()
			if err != nil {
				return nil, fmt.Errorf("%v:%v - failure on Client.NewSession() - details: %v", s.Ip, s.Port, err.Error())
			}
			defer s.Session.Close()
			var b bytes.Buffer
			s.Session.Stdout = &b
			if err := s.Session.Run(c); err != nil {
				return nil, fmt.Errorf("failed to run: %v >> %v", c, err.Error())
			} else {
				result[c] = b.String()
			}
		}
	}

	return result, nil
}

//Disconnect closes the ssh sessoin and connection.
func (s *Endpoint) Disconnect() {
	s.Session.Close()
	s.Client.Close()
}

func validateIpAddress(ip string) error {
	ipSegments := strings.Split(ip, ".")
	if len(ipSegments) != 4 {
		return fmt.Errorf("provided ip: %v - ip address is not formatted properly", ip)
	}
	for _, seg := range ipSegments {
		num, err := strconv.Atoi(seg)
		if err != nil {
			return fmt.Errorf("provided ip: %v - ip address includes wrong values: %v", ip, seg)
		} else {
			if num < 0 || num > 255 {
				return fmt.Errorf("provided ip: %v - ip address includes wrong values: %v", ip, seg)
			}
		}
	}

	return nil
}

//
func validateNode(s *Endpoint) error {
	s.Timeout = 30
	if err := validateIpAddress(s.Ip); err != nil {
		return err
	}
	if _, err := strconv.Atoi(s.Port); err != nil {
		log.Printf("provided port: %v - wrong port number, defaulting to 22", s.Port)
		s.Port = "22"
	}

	return nil
}

func (s *Endpoint) gmreLogin() error {
	if _, err := writeBuff("tools gmre", s.SshIn); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on Run(tools gmre) - details: %v", s.Ip, s.Port, err.Error())
	}

	if _, err := readBuff([]string{"username:"}, s.SshOut, 15); err != nil {
		return fmt.Errorf("%v:%v - failure on gmre login - readBuff(username:) - details: %v", s.Ip, s.Port, err.Error())
	}

	if _, err := writeBuff("gmre\r", s.SshIn); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on username(gmre) - details: %v", s.Ip, s.Port, err.Error())
	}

	if _, err := readBuff([]string{"password:"}, s.SshOut, 15); err != nil {
		return fmt.Errorf("%v:%v - failure on gmre login - readBuff(password:) - details: %v", s.Ip, s.Port, err.Error())
	}

	if _, err := writeBuff("gmre\r", s.SshIn); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on password(gmre) - details: %v", s.Ip, s.Port, err.Error())
	}

	if _, err := readBuff([]string{"]#"}, s.SshOut, 15); err != nil {
		return fmt.Errorf("%v:%v - failure on gmre login - readBuff(]#) - details: %v", s.Ip, s.Port, err.Error())
	}

	return nil
}

func (s *Endpoint) gmreLogout() error {
	if _, err := writeBuff("quit\r", s.SshIn); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on Run(quit) - details: %v", s.Ip, s.Port, err.Error())
	}

	if _, err := readBuff([]string{s.Name + "#"}, s.SshOut, 15); err != nil {
		return fmt.Errorf("%v:%v - failure on gmre login - readBuff(cli prompt) - details: %v", s.Ip, s.Port, err.Error())
	}

	return nil
}

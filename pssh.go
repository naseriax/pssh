package pssh

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	prompt     = `([\w\-\#\+\%\/\(\d\)\[\d\]]+#\s)|(ACT-OSE \$)`
	username   = `(?i)(user( ?name)?)\s?:`
	password   = `(?i)(pass( ?word)?)\s?:`
	agreement  = `\(\s?(?i:yes|y)\s?\/\s?(?i:no|n)\s?\)\s?[:?]?`
	authFail   = `(?i)(failed\.)`
	srosPrompt = `(\w+\:\w+\@[\w\-\#\+\%\/\(\d\)\[\d\]]+#\s)`
)

type Endpoint struct {
	ViaTunnel bool
	Ip        string
	Name      string
	UserName  string
	Password  string
	Vars      map[string][]string
	Port      string
	SshOut    io.Reader
	SshIn     io.WriteCloser
	Timeout   int
	Client    *ssh.Client
	Session   *ssh.Session
	Kind      string //Accepted values: BASH, PSS, OSE, PSD,...
}

func readBuffForString(happyExpectations, sadExpectations []*regexp.Regexp, sshOut io.Reader, buffRead chan<- []string, errChan chan []error) {
	buf := make([]byte, 1000)
	waitingString := ""
	for {
		n, err := sshOut.Read(buf)
		if err != nil && err != io.EOF {
			fmt.Println(err)
			return
		}
		if err == io.EOF || n == 0 {
			return
		}
		waitingString = strings.Join([]string{waitingString, string(buf[:n])}, "")
		for _, re := range happyExpectations {
			if r := re.FindString(waitingString); r != "" {
				buffRead <- []string{waitingString, fmt.Sprintf("%v", re), r}
				return
			}
		}
		for _, re := range sadExpectations {
			if r := re.FindString(waitingString); r != "" {
				errChan <- []error{fmt.Errorf(r), fmt.Errorf("%v - %v", re, waitingString)}
				return
			}
		}
	}
}

func readBuff(happyExpectations, sadExpectations []*regexp.Regexp, sshOut io.Reader, timeoutSeconds int) ([]string, []error) {
	errChan := make(chan []error)
	ch := make(chan []string)
	go func(happyExpectations, sadExpectations []*regexp.Regexp, sshOut io.Reader, errChan chan []error) {
		buffRead := make(chan []string)
		go readBuffForString(happyExpectations, sadExpectations, sshOut, buffRead, errChan)
		select {
		case ret := <-buffRead:
			ch <- ret
		case <-time.After(time.Duration(timeoutSeconds) * time.Second):
		}
	}(happyExpectations, sadExpectations, sshOut, errChan)
	select {
	case data := <-ch:
		return data, nil
	case err := <-errChan:
		return []string{}, err
	case <-time.After(time.Duration(timeoutSeconds) * time.Second):
		return []string{}, []error{fmt.Errorf("timedout on: %+v , %+v ", happyExpectations, sadExpectations), fmt.Errorf("")}
	}
}

func writeBuff(command string, sshIn io.WriteCloser) (int, error) {
	returnCode, err := sshIn.Write([]byte(command + "\n"))
	return returnCode, err
}

// Connect connects to the specified server and opens a session (Filling the Client and Session fields in SshAgent struct).
func (s *Endpoint) Connect() error {
	if err := validateNode(s); err != nil {
		return err
	}

	var err error
	config := &ssh.ClientConfig{
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         time.Duration(s.Timeout) * time.Second,
	}

	sshUser := "cli"
	sshPass := "cli"

	k := strings.ToLower(s.Kind)

	switch k {
	case "bash", "ose":
		sshUser = s.UserName
		sshPass = s.Password
	}

	config.User = sshUser
	config.Auth = []ssh.AuthMethod{
		ssh.Password(sshPass),
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

	switch k {
	case "pss", "psd", "gmre", "sros":
		if err := s.cliLogin(); err != nil {
			return err
		}
	case "ose":
		if err := s.oseLogin(); err != nil {
			return err
		}
	}

	return nil
}

// CliLogin does the special login sequence needed to login to PSS cli.
func (s *Endpoint) cliLogin() error {
	prompt_re := regexp.MustCompile(prompt)
	username_re := regexp.MustCompile(username)
	password_re := regexp.MustCompile(password)
	agreement_re := regexp.MustCompile(agreement)
	authFail_re := regexp.MustCompile(authFail)
	srosPrompt_re := regexp.MustCompile(srosPrompt)

	var err error
	modes := ssh.TerminalModes{
		ssh.ECHO:          0,
		ssh.TTY_OP_ISPEED: 14400,
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

	if strings.ToLower(s.Kind) == "sros" {
		if _, err := readBuff([]*regexp.Regexp{srosPrompt_re}, []*regexp.Regexp{}, s.SshOut, 6); err != nil {
			s.Session.Close()
			return fmt.Errorf("%v:%v - failure on readBuff(srosPrompt) - details: %v", s.Ip, s.Port, fmt.Errorf("%v - %v", err[0], err[1]))
		}

		return nil
	}

	if _, err := readBuff([]*regexp.Regexp{username_re}, []*regexp.Regexp{}, s.SshOut, 6); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on readBuff(username) - details: %v", s.Ip, s.Port, fmt.Errorf("%v - %v", err[0], err[1]))
	}

	if _, err := writeBuff(s.UserName, s.SshIn); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on writeBuff(s.UserName) - details: %v", s.Ip, s.Port, err.Error())
	}

	if _, err := readBuff([]*regexp.Regexp{password_re}, []*regexp.Regexp{}, s.SshOut, 6); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on readBuff(Password) - details: %v", s.Ip, s.Port, fmt.Errorf("%v - %v", err[0], err[1]))
	}

	if _, err := writeBuff(s.Password, s.SshIn); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on writeBuff(s.Password) - details: %v", s.Ip, s.Port, err.Error())
	}

	if s.Kind == "PSS" || s.Kind == "GMRE" {
		if response, err := readBuff([]*regexp.Regexp{agreement_re, prompt_re}, []*regexp.Regexp{authFail_re}, s.SshOut, 6); err != nil {
			return fmt.Errorf("%v:%v - failure on readBuff(Y/N) - details: %v", s.Ip, s.Port, fmt.Errorf("%v - %v", err[0], err[1]))
		} else if response[1] == agreement {

			if _, err := writeBuff("yes", s.SshIn); err != nil {
				s.Session.Close()
				return fmt.Errorf("%v:%v - failure on writeBuff(yes) - details: %v", s.Ip, s.Port, err.Error())
			}
			if _, err := readBuff([]*regexp.Regexp{prompt_re}, []*regexp.Regexp{}, s.SshOut, 4); err != nil {
				s.Session.Close()
				return fmt.Errorf("%v:%v - failure on readBuff(#) (INIT) - details: %v", s.Ip, s.Port, fmt.Errorf("%v - %v", err[0], err[1]))
			}
		}
	}

	if _, err := writeBuff("paging status disable", s.SshIn); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on writeBuff(Page Status Disable) - details: %v", s.Ip, s.Port, err.Error())
	}

	if s.Kind == "PSS" || s.Kind == "GMRE" || s.Kind == "PSD" {
		if _, err := readBuff([]*regexp.Regexp{prompt_re}, []*regexp.Regexp{}, s.SshOut, 4); err != nil {
			s.Session.Close()
			return fmt.Errorf("%v:%v - failure on readBuff(#) (END) - details: %v", s.Ip, s.Port, fmt.Errorf("%v - %v", err[0], err[1]))
		}
	}
	return nil
}

// oseLogin first logs in to the BASH cli and then jump to the OSE prompt.
func (s *Endpoint) oseLogin() error {
	prompt_re := regexp.MustCompile(prompt)
	var err error
	modes := ssh.TerminalModes{
		ssh.ECHO:          0,
		ssh.TTY_OP_ISPEED: 14400,
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
	if _, err := readBuff([]*regexp.Regexp{prompt_re}, []*regexp.Regexp{}, s.SshOut, 6); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on readBuff(#) - details: %v", s.Ip, s.Port, fmt.Errorf("%v - %v", err[0], err[1]))
	}

	if _, err := writeBuff("ose", s.SshIn); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on writeBuff(ose) - details: %v", s.Ip, s.Port, err.Error())
	}

	if _, err := writeBuff("\n \n", s.SshIn); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on writeBuff(newLines) - details: %v", s.Ip, s.Port, err.Error())
	}

	if _, err := readBuff([]*regexp.Regexp{prompt_re}, []*regexp.Regexp{}, s.SshOut, 6); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on readBuff(ACT-OSE $ ) - details: %v", s.Ip, s.Port, fmt.Errorf("%v - %v", err[0], err[1]))
	}

	return nil
}

// Run executes the given cli command on the opened session.
func (s *Endpoint) Run(args ...string) (map[string]string, error) {

	prompt_re := regexp.MustCompile(prompt)
	expectedPrompt := []*regexp.Regexp{prompt_re}

	if strings.ToLower(s.Kind) == "sros" {
		srosPrompt_re := regexp.MustCompile(srosPrompt)
		expectedPrompt = []*regexp.Regexp{srosPrompt_re}
	}

	if len(args) > 1 {
		expectedPrompt = append(expectedPrompt, regexp.MustCompile(args[1]))
	}

	result := map[string]string{}
	if s.Kind == "PSS" || s.Kind == "PSD" || s.Kind == "GMRE" || s.Kind == "SROS" {
		if s.Kind == "GMRE" {
			if err := s.gmreLogin(); err != nil {
				return nil, err
			}
			args[0] += "\r"
		}

		if _, err := writeBuff(args[0], s.SshIn); err != nil {
			s.Session.Close()
			return nil, fmt.Errorf("%v:%v - failure on Run(%v) - details: %v", s.Ip, s.Port, args[0], err.Error())
		}

		data, err := readBuff(expectedPrompt, []*regexp.Regexp{}, s.SshOut, 15)
		if err != nil {
			return nil, fmt.Errorf("%v:%v - failure on Run(%v) - readBuff(%v#) - details: %v", s.Ip, s.Port, s.Name, args[0], fmt.Errorf("%v - %v", err[0], err[1]))
		}
		result[args[0]] = data[0]
	}

	if s.Kind == "GMRE" || s.Kind == "PSD" {
		if err := s.gmreLogout(); err != nil {
			return nil, err
		}

	} else if s.Kind == "BASH" {
		var err error
		s.Session, err = s.Client.NewSession()
		if err != nil {
			return nil, fmt.Errorf("%v:%v - failure on Client.NewSession() - details: %v", s.Ip, s.Port, err.Error())
		}
		defer s.Session.Close()
		var b bytes.Buffer
		s.Session.Stdout = &b
		if err := s.Session.Run(args[0]); err != nil {
			return nil, fmt.Errorf("failed to run: %v >> %v", args[0], err.Error())
		} else {
			result[args[0]] = b.String()
		}
	} else if s.Kind == "OSE" {

		if _, err := writeBuff(args[0], s.SshIn); err != nil {
			s.Session.Close()
			return nil, fmt.Errorf("%v:%v - failure on Run(%v) - details: %v", s.Ip, s.Port, args[0], err.Error())
		}

		data, err := readBuff(expectedPrompt, []*regexp.Regexp{}, s.SshOut, 15)
		if err != nil {
			return nil, fmt.Errorf("%v:%v - failure on Run(%v) - readBuff(%v#) - details: %v", s.Ip, s.Port, s.Name, args[0], fmt.Errorf("%v - %v", err[0], err[1]))
		}
		result[args[0]] = data[0]

		s.Session.Signal(ssh.SIGINT)
		time.Sleep(3 * time.Second)
	}

	return result, nil
}

// Disconnect closes the ssh sessoin and connection.
func (s *Endpoint) Disconnect() {
	if s.Kind == "PSS" || s.Kind == "PSS23.6" {
		s.Session.Close()
	}

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

// gmreLogin logs in to the PSS cli and then switches to gmre by sending "tools gmre" command.
func (s *Endpoint) gmreLogin() error {

	prompt_re := regexp.MustCompile(prompt)
	username_re := regexp.MustCompile(username)
	password_re := regexp.MustCompile(password)
	if _, err := writeBuff("tools gmre", s.SshIn); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on Run(tools gmre) - details: %v", s.Ip, s.Port, err.Error())
	}

	if _, err := readBuff([]*regexp.Regexp{username_re}, []*regexp.Regexp{}, s.SshOut, 15); err != nil {
		return fmt.Errorf("%v:%v - failure on gmre login - readBuff(username:) - details: %v", s.Ip, s.Port, fmt.Errorf("%v - %v", err[0], err[1]))
	}

	if _, err := writeBuff("gmre\r", s.SshIn); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on username(gmre) - details: %v", s.Ip, s.Port, err.Error())
	}

	if _, err := readBuff([]*regexp.Regexp{password_re}, []*regexp.Regexp{}, s.SshOut, 15); err != nil {
		return fmt.Errorf("%v:%v - failure on gmre login - readBuff(password:) - details: %v", s.Ip, s.Port, fmt.Errorf("%v - %v", err[0], err[1]))
	}

	if _, err := writeBuff("gmre\r", s.SshIn); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on password(gmre) - details: %v", s.Ip, s.Port, err.Error())
	}

	if _, err := readBuff([]*regexp.Regexp{prompt_re}, []*regexp.Regexp{}, s.SshOut, 15); err != nil {
		return fmt.Errorf("%v:%v - failure on gmre login - readBuff(]#) - details: %v", s.Ip, s.Port, fmt.Errorf("%v - %v", err[0], err[1]))
	}

	return nil
}

// gmreLogout sends quit command to PSS gmre and PSD.
func (s *Endpoint) gmreLogout() error {
	prompt_re := regexp.MustCompile(prompt)
	if _, err := writeBuff("quit\r", s.SshIn); err != nil {
		s.Session.Close()
		return fmt.Errorf("%v:%v - failure on Run(quit) - details: %v", s.Ip, s.Port, err.Error())
	}

	if _, err := readBuff([]*regexp.Regexp{prompt_re}, []*regexp.Regexp{}, s.SshOut, 15); err != nil {
		return fmt.Errorf("%v:%v - failure on gmre login - readBuff(cli prompt) - details: %v", s.Ip, s.Port, fmt.Errorf("%v - %v", err[0], err[1]))
	}

	return nil
}

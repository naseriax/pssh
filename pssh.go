package pssh

import (
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type Nokia_1830PSS struct {
	Ip       string
	Name     string
	UserName string
	Password string
	Port     string
	SshOut   io.Reader
	SshIn    io.WriteCloser
	Timeout  int
	Client   *ssh.Client
	Session  *ssh.Session
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
			log.Printf("Timedout: %v", whattoexpect)
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
func (s *Nokia_1830PSS) connect() error {
	var err error
	config := &ssh.ClientConfig{
		User: "cli",
		Auth: []ssh.AuthMethod{
			ssh.Password("cli"),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         time.Duration(s.Timeout) * time.Second,
	}
	s.Client, err = ssh.Dial("tcp", fmt.Sprintf("%v:%v", s.Ip, s.Port), config)
	if err != nil {
		return fmt.Errorf("%v:%v - %v", s.Ip, s.Port, err.Error())
	}

	if err := s.cliLogin(); err != nil {
		return err
	}
	return nil
}

//parseNeName extracts the actuall ne name and fills ne.Name variable.
func (s *Nokia_1830PSS) parseNeName(lines []string) string {
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
func (s *Nokia_1830PSS) cliLogin() error {
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
func (s *Nokia_1830PSS) Run(cmd string) (string, error) {
	if _, err := writeBuff(cmd, s.SshIn); err != nil {
		s.Session.Close()
		return "", fmt.Errorf("%v:%v - failure on Exec(%v) - details: %v", s.Ip, s.Port, cmd, err.Error())
	}

	data, err := readBuff([]string{s.Name + "#"}, s.SshOut, 15)
	if err != nil {
		return "", fmt.Errorf("%v:%v - failure on Exec(%v) - readBuff(%v#) - details: %v", s.Ip, s.Port, s.Name, cmd, err.Error())
	}

	return data, nil
}

//Disconnect closes the ssh sessoin and connection.
func (s *Nokia_1830PSS) Disconnect() {
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
func validateNode(ne *Nokia_1830PSS) error {
	ne.Timeout = 30
	if err := validateIpAddress(ne.Ip); err != nil {
		return err
	}
	if _, err := strconv.Atoi(ne.Port); err == nil {
		return fmt.Errorf("provided port: %v - wrong port number", ne.Port)
	}

	return nil
}

//Init initialises the ssh connection and returns the reusable ssh agent.
func Init(ne *Nokia_1830PSS) error {
	if err := validateNode(ne); err != nil {
		return err
	}

	if err := ne.connect(); err != nil {
		return err
	}
	return nil
}

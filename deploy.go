package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"syscall"

	"github.com/fatih/color"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

var cmd = flag.String("c", "", "command to execute in the server (no batch)")
var batch = flag.String("b", "", "path to a file with commands to execute in the server")
var ip = flag.String("ip", "", "ip address")
var certificate = flag.String("cert", "", "path to certificate")
var userp = flag.String("u", "", "user for connecting")
var files = flag.String("f", "", "path to a file with files to transfer to the server")

func main() {

	flag.Parse()

	if *ip == "" {
		flag.PrintDefaults()
		os.Exit(1)
	}

	// We use session just for executing commands passed throuh terminal
	// We use client for for executing batch commands
	client, session, err := connectToServer(ip, *certificate, *userp)

	defer session.Close()
	defer client.Close()

	if err != nil {
		panic("Failed connection: " + err.Error())
	}

	if *files != "" {
		_, err := uploadFiles(client, *files)
		if err != nil {
			panic("Error uploading files: " + err.Error())
		}
	}

	if *cmd != "" {
		out, err := session.CombinedOutput(*cmd)

		if err != nil {
			panic("Failed to run: " + err.Error())
		}

		log.Println(string(out))
	}

	if *batch != "" {
		out, err := executeBatch(client, *batch)
		if err != nil {
			panic("Error in batch file" + err.Error())
		}

		log.Println(out)
		color.Set(color.FgGreen)
		log.Println("Deploy Done!!!")
		color.Unset()
	}

}

func uploadFiles(client *ssh.Client, path string) (resp string, error error) {
	file, err := os.Open(path)
	if err != nil {
		return "nok", err
	}
	defer file.Close()

	sftp, err := sftp.NewClient(client)
	if err != nil {
		return "nok", err
	}

	defer sftp.Close()

	color.Set(color.FgYellow)
	scanner := bufio.NewScanner(file)
	c := make(chan string)
	// For avoiding deadlocks waiting response in channels
	processingFiles := 0
	for scanner.Scan() {
		// Checking if we want to exclude that file
		if scanner.Text()[:2] == "//" {
			continue
		}
		processingFiles++
		log.Println("Uploading " + filepath.Base(scanner.Text()))

		go func(path string) {
			srcFile, err := os.Open(path)
			if err != nil {
				c <- "Error: " + err.Error()
			}

			defer srcFile.Close()

			dstFile, err := sftp.Create(filepath.Base(path))
			if err != nil {
				c <- "Error: " + err.Error()
			}
			defer dstFile.Close()

			buf := make([]byte, 32*1024)
			for {
				n, _ := srcFile.Read(buf)
				if n == 0 {
					break
				}
				dstFile.Write(buf)
			}

			c <- filepath.Base(path) + " Uploaded!!!"

		}(scanner.Text())
	}

	for i := range c {
		log.Println(i)
		processingFiles--
		if processingFiles == 0 {
			break
		}
	}

	color.Unset()
	return "ok", nil
}

func executeBatch(client *ssh.Client, path string) (resp string, error error) {
	file, err := os.Open(path)
	if err != nil {
		return "nok", err
	}

	defer file.Close()
	scanner := bufio.NewScanner(file)
	var stdoutBuf bytes.Buffer
	color.Set(color.FgYellow)
	for scanner.Scan() {
		// Checking if we want to exclude that command
		if scanner.Text()[:2] == "//" {
			continue
		}
		session, err := client.NewSession()
		if err != nil {
			log.Fatal(err)
		}
		defer session.Close()

		session.Stdout = &stdoutBuf
		session.Run(scanner.Text())
		log.Println("Executing: " + scanner.Text())
	}
	color.Unset()
	if err := scanner.Err(); err != nil {
		return "nok", err
	}

	return stdoutBuf.String(), nil
}

func connectToServer(host *string, certificate string, userp string) (*ssh.Client, *ssh.Session, error) {
	promptLogin := true

	if certificate != "" && userp != "" {
		promptLogin = false
	}

	var user string
	var bytePassword []byte
	var sshConfig ssh.ClientConfig
	var err error

	if promptLogin {
		if userp != "" {
			user = userp
		} else {
			fmt.Print("User: ")
			fmt.Scanf("%s\n", &user)
		}
		fmt.Print("Password: ")
		// fmt.Scanf("%s\n", &pass)
		bytePassword, err = terminal.ReadPassword(int(syscall.Stdin))
		fmt.Println()

		if err != nil {
			panic("Failed getting password: " + err.Error())
		}

		sshConfig = ssh.ClientConfig{
			User: user,
			Auth: []ssh.AuthMethod{
				ssh.Password(string(bytePassword)),
			},
		}
	} else {
		sshConfig = ssh.ClientConfig{
			User: userp,
			Auth: []ssh.AuthMethod{
				publicKeyFile(certificate),
			},
		}
	}

	sshConfig.HostKeyCallback = ssh.InsecureIgnoreHostKey()
	client, err := ssh.Dial("tcp", *host+":22", &sshConfig)
	if err != nil {
		return nil, nil, err
	}

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, nil, err
	}

	return client, session, nil
}

func publicKeyFile(file string) ssh.AuthMethod {
	buffer, err := ioutil.ReadFile(file)
	if err != nil {
		return nil
	}

	key, err := ssh.ParsePrivateKey(buffer)
	if err != nil {
		return nil
	}
	return ssh.PublicKeys(key)
}

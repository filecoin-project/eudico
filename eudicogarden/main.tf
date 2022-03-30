terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 3.27"
    }
  }

  required_version = ">= 0.14.9"
}

variable "awsprops" {
  default = {
    region       = "us-east-1"
    ami          = "ami-0c1bea58988a989155"
    publicip     = true
    secgroupname = "eudico-sec-group"
  }
}

variable "num_nodes" {
        default = 3
} 

provider "aws" {
  region = lookup(var.awsprops, "region")
}

resource "aws_security_group" "project-iac-sg" {
  name        = lookup(var.awsprops, "secgroupname")
  description = lookup(var.awsprops, "secgroupname")

  ingress {
    from_port   = 22
    protocol    = "tcp"
    to_port     = 22
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    from_port   = 30201
    protocol    = "tcp"
    to_port     = 30201
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_key_pair" "spot_key" {
  key_name   = "spot_key"
  public_key = file("~/.ssh/id_rsa.pub")
}

resource "aws_instance" "eudico-bootstrap" {
  ami           = "ami-0b0ea68c435eb488d"
  instance_type = "m5a.large"
  vpc_security_group_ids = [
    aws_security_group.project-iac-sg.id
  ]
  key_name                    = "spot_key"
  associate_public_ip_address = lookup(var.awsprops, "publicip")

  tags = {
    Name = "eudico-bootstrap"
  }

  connection {
    type        = "ssh"
    user        = "ubuntu"
    private_key = file("~/.ssh/id_rsa")
    host        = self.public_ip
  }
  
  provisioner "remote-exec" {
    script = "./entrypoint.sh"
  }

  depends_on = [aws_security_group.project-iac-sg]
}

resource "aws_instance" "eudico-node" {
  count         = var.num_nodes
  ami           = "ami-0b0ea68c435eb488d"
  instance_type = "m5a.large"
  vpc_security_group_ids = [
    aws_security_group.project-iac-sg.id
  ]
  key_name                    = "spot_key"
  associate_public_ip_address = lookup(var.awsprops, "publicip")

  tags = {
    Name = "eudico-node-${count.index}"
  }

  connection {
    type        = "ssh"
    user        = "ubuntu"
    private_key = file("~/.ssh/id_rsa")
    host        = self.public_ip
  }
  
  provisioner "remote-exec" {
    script = "./entrypoint.sh"
  }

  depends_on = [aws_security_group.project-iac-sg]
}



terraform {
  required_version = ">= 1.6"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

variable "region" {
  type    = string
  default = "eu-west-2"
}

provider "aws" {
  region = var.region
}

resource "aws_instance" "web" {
  ami           = "ami-cf25ec84d8dbc7425"
  instance_type = "t3.micro"
  count         = 2
  tags = {
    Name = "${local.prefix}-web"
    Team = "platform"
  }
}

resource "aws_s3_bucket" "assets" {
  bucket = "example-assets"
  versioning {
    enabled = true
  }
}

output "web_ips" {
  value     = aws_instance.web[*].public_ip
  sensitive = false
}

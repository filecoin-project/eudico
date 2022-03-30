output "eudico_nodes_ip" {
  description = "Eudico nodes public ip"
  value       = aws_instance.eudico-node.*.public_ip
}

output "eudico_bootstrap_ip" {
  description = "Eudico bootstrap public ip"
  value       = aws_instance.eudico-bootstrap.public_ip
}

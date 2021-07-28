STACK_NAME="deploy-task"

build:
	go build -o deploy cmd/deploy/main.go

test:
	go test ./...

bootstrap-env:
	aws cloudformation create-stack --stack-name ${STACK_NAME} --template-body file://bootstrap-cloudformation.yaml
	aws cloudformation wait stack-create-complete --stack-name ${STACK_NAME}
	@echo "Your LoadBalancer DNS is:"
	aws cloudformation describe-stacks --stack-name ${STACK_NAME} --query "Stacks[0].Outputs[?OutputKey=='LoadBalancerDNS'].OutputValue" --output text

destroy-env:
	aws cloudformation delete-stack --stack-name ${STACK_NAME}
	aws cloudformation wait stack-delete-complete --stack-name ${STACK_NAME}

.PHONY: build test bootstrap-env destroy-env

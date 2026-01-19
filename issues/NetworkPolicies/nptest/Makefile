all: init test clean

init:
	@echo "Creating namespaces..."
	oc apply -f namespaces/
	@echo ""
	@echo "Waiting for namespaces to be active..."
	oc wait --for=jsonpath='{.status.phase}'=Active namespace/nptest1 namespace/nptest2 --timeout=30s
	@echo ""
	@echo "Creating pods..."
	oc apply -f pods/
	@echo ""
	@echo "Waiting for pods to be ready..."
	oc wait --for=condition=ready pod --all -n nptest1 --timeout=60s
	oc wait --for=condition=ready pod --all -n nptest2 --timeout=60s
	@echo ""
	@echo "Done!"

clean:
	oc delete ns nptest1 nptest2

clean-np:
	@echo "Deleting all NetworkPolicies..."
	oc delete networkpolicy --all -n nptest1
	oc delete networkpolicy --all -n nptest2

test:
	@echo "Running all tests..."
	@echo ""
	./tests/test-01-default-allow.sh
	./tests/test-02-deny-all.sh
	./tests/test-03-same-namespace.sh
	./tests/test-04-specific-cross-ns.sh
	@echo ""
	@echo "All tests completed!"
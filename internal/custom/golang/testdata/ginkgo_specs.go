package sample

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("UserService", func() {
	var svc *UserService

	BeforeEach(func() {
		svc = NewUserService()
	})

	AfterEach(func() {
		svc.Close()
	})

	Context("when creating a user", func() {
		It("returns the created user", func() {
			u, err := svc.Create("bob")
			Expect(err).ToNot(HaveOccurred())
			Expect(u.Name).To(Equal("bob"))
		})

		It("rejects empty names", func() {
			_, err := svc.Create("")
			Expect(err).To(HaveOccurred())
		})
	})

	When("deleting a user", func() {
		Specify("the user is gone", func() {
			Expect(svc.Delete(1)).To(Succeed())
		})
	})

	FIt("can be focused", func() {
		Expect(true).To(BeTrue())
	})

	PIt("is pending", func() {
		Expect(false).To(BeFalse())
	})
})

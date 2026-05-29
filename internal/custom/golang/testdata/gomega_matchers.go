package sample

import (
	. "github.com/onsi/gomega"
)

func checkValues(g Gomega) {
	Expect(2 + 2).To(Equal(4))
	Expect("hello").ToNot(BeEmpty())
	Expect([]int{1, 2, 3}).To(ContainElement(2))
	Ω(nilable()).Should(BeNil())
	Expect(err).ShouldNot(HaveOccurred())
	Eventually(poll).Should(BeTrue())
	Consistently(poll).ShouldNot(BeFalse())
	Expect(value).NotTo(Equal(0))
}

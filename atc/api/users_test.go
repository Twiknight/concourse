package api_test

import (
	"github.com/concourse/concourse/atc/api/accessor/accessorfakes"
	"net/http"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = FDescribe("Users API", func() {

	var (
		response   *http.Response
		fakeaccess *accessorfakes.FakeAccess
	)

	BeforeEach(func() {
		fakeaccess = new(accessorfakes.FakeAccess)
	})

	Context("GET /api/v1/users", func() {

		JustBeforeEach(func() {
			fakeAccessor.CreateReturns(fakeaccess)

			req, err := http.NewRequest("GET", server.URL+"/api/v1/users", nil)
			Expect(err).NotTo(HaveOccurred())

			response, err = client.Do(req)
			Expect(err).NotTo(HaveOccurred())
		})

		Context("when authenticated", func() {

			BeforeEach(func() {
				fakeaccess.IsAuthenticatedReturns(true)
			})

			Context("not an admin", func() {

				It("returns 403", func() {
					Expect(response.StatusCode).To(Equal(http.StatusForbidden))
				})

			})

		})

		Context("not authenticated", func() {

			BeforeEach(func() {
				fakeaccess.IsAuthenticatedReturns(false)
			})

			It("returns 401", func() {
				Expect(response.StatusCode).To(Equal(http.StatusUnauthorized))
			})

		})

	})

})

from playwright.sync_api import Page
















































def test_checkout_confirms(page: Page):
    page.goto("/checkout")
    page.fill("#card", "4242 4242 4242 4242")
    page.click("#pay")

    page.wait_for_selector("#confirm", timeout=8000)

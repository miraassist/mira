"""Basic acceptance tests of Mira's page: it opens, every page of every group
opens without an error, the way back works, and someone who is no admin sees
only their part. Deliberately few, and without texts, so the page may change
its words and looks."""

from playwright.sync_api import expect


def test_start_page_has_the_groups_and_my_card(admin):
    expect(admin.locator("mira-home .home-card")).to_have_count(4)
    expect(admin.locator("mira-home .home-person")).to_be_visible()


def test_every_page_opens_without_error(admin):
    for group in ["daily", "setup", "abilities", "help"]:
        admin.locator(f"mira-home .home-card[data-group={group}] h2").click()
        expect(admin.locator("mira-nav .page-head h1")).to_be_visible()
        tabs = admin.locator("mira-nav .tabs button")
        for i in range(max(tabs.count(), 1)):
            if tabs.count():
                tabs.nth(i).click()
                expect(tabs.nth(i)).to_have_class("active")
            expect(admin.locator("#view section.card").first).to_be_visible()
            expect(admin.locator("#msg.err")).to_have_count(0)
        admin.locator("mira-nav button.back").click()
        expect(admin.locator("mira-home .home-grid")).to_be_visible()


def test_my_card_opens(admin):
    admin.locator("mira-home .home-person h2").click()
    expect(admin.locator("#me-settings")).to_be_visible()
    expect(admin.locator("#voice")).to_be_visible()


def test_who_is_no_admin_sees_only_their_part(user):
    groups = user.locator("mira-home .home-card")
    expect(groups).to_have_count(2)
    expect(user.locator("mira-home .home-card[data-group=setup]")).to_have_count(0)
    expect(user.locator("mira-home .home-card[data-group=abilities]")).to_have_count(0)
    expect(user.locator("mira-home .home-person")).to_be_visible()

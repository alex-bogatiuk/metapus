import { test, expect } from '@playwright/test';

test.describe('Nomenclature Catalog', () => {
    test('should load nomenclature page and show title', async ({ page }) => {
        // Navigate to nomenclature page
        // In a real test, we would use baseURL from config
        await page.goto('http://localhost:3000/catalogs/nomenclatures');

        // Wait for title to load
        const title = page.locator('h1');
        await expect(title).toBeVisible();

        // Take screenshot for debugging
        await page.screenshot({ path: 'playwright-report/nomenclature-page.png' });

        console.log('Page title:', await title.innerText());
    });

    test('should open create form', async ({ page }) => {
        await page.goto('http://localhost:3000/catalogs/nomenclatures');

        // Find create button (assuming it has text "Create" or a plus icon)
        const createButton = page.locator('button:has-text("Создать"), button:has-text("Add")');
        if (await createButton.isVisible()) {
            await createButton.click();
            // Verify that the form appeared (e.g., by presence of "Name" field)
            await expect(page.locator('label:has-text("Наименование"), label:has-text("Name")')).toBeVisible();
        }
    });
});

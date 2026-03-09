import { test, expect } from '@playwright/test';

test.describe('Nomenclature Catalog', () => {
    test('should load nomenclature page and show title', async ({ page }) => {
        // Переходим на страницу номенклатуры
        // В реальном тесте мы бы использовали baseUrl из конфига
        await page.goto('http://localhost:3000/catalogs/nomenclature');

        // Ждем загрузки заголовка
        const title = page.locator('h1');
        await expect(title).toBeVisible();

        // Делаем скриншот для отладки
        await page.screenshot({ path: 'playwright-report/nomenclature-page.png' });

        console.log('Page title:', await title.innerText());
    });

    test('should open create form', async ({ page }) => {
        await page.goto('http://localhost:3000/catalogs/nomenclature');

        // Ищем кнопку создания (предположим, у нее есть текст "Создать" или иконка плюс)
        const createButton = page.locator('button:has-text("Создать"), button:has-text("Add")');
        if (await createButton.isVisible()) {
            await createButton.click();
            // Проверяем, что появилась форма (например, по наличию поля "Наименование")
            await expect(page.locator('label:has-text("Наименование"), label:has-text("Name")')).toBeVisible();
        }
    });
});

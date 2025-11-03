from locust import HttpUser, task, between, events
import random
import time
import os
import json

# Configuration from environment variables
MODE = os.getenv("TEST_MODE", "dynamodb").lower()  # mysql or dynamodb
print(f"Running load test for: {MODE} backend")

# Global storage for created cart IDs
cart_ids = []
MAX_CART_IDS = 1000  # Keep only last 1000 cart IDs in memory


class ShoppingCartUser(HttpUser):
    """
    Simulates a user performing shopping cart operations.
    
    Task weights:
    - 30% create new carts
    - 40% add items to existing carts
    - 30% retrieve cart details
    """
    
    # Wait time between tasks (simulates user think time)
    wait_time = between(0.5, 2.0)

    def on_start(self):
        """Called when a simulated user starts."""
        # Create initial cart for this user
        self.my_cart_id = None
        self.create_cart()

    @task(3)
    def create_cart(self):
        """Create a new shopping cart (30% of operations)"""
        customer_id = random.randint(1, 10000)

        with self.client.post(
            "/shopping-carts",
            json={"customer_id": customer_id},
            catch_response=True,
            name="POST /shopping-carts (create)"
        ) as response:
            if response.status_code == 201:
                try:
                    data = response.json()
                    cart_id = data.get("shopping_cart_id")

                    if cart_id:
                        # Store cart ID for this user
                        self.my_cart_id = cart_id

                        # Add to global list (with limit)
                        global cart_ids
                        cart_ids.append(cart_id)
                        if len(cart_ids) > MAX_CART_IDS:
                            cart_ids.pop(0)

                        response.success()
                    else:
                        response.failure("No cart_id in response")
                except json.JSONDecodeError:
                    response.failure("Invalid JSON response")
            else:
                response.failure(f"Failed with status {response.status_code}")

    @task(4)
    def add_items_to_cart(self):
        """Add items to an existing cart (40% of operations)"""
        # Try to use this user's cart first, otherwise pick a random one
        cart_id = self.my_cart_id

        if not cart_id and cart_ids:
            cart_id = random.choice(cart_ids)

        if not cart_id:
            # No carts available, create one first
            self.create_cart()
            return

        product_id = random.randint(1, 100)
        quantity = random.randint(1, 5)

        with self.client.post(
            f"/shopping-carts/{cart_id}/items",
            json={
                "product_id": product_id,
                "quantity": quantity
            },
            catch_response=True,
            name="POST /shopping-carts/{id}/items (add)"
        ) as response:
            if response.status_code == 204:
                response.success()
            elif response.status_code == 404:
                # Cart not found, remove from our list
                if cart_id in cart_ids:
                    cart_ids.remove(cart_id)
                if self.my_cart_id == cart_id:
                    self.my_cart_id = None
                response.failure("Cart not found")
            else:
                response.failure(f"Failed with status {response.status_code}")

    @task(3)
    def get_cart(self):
        """Retrieve cart details (30% of operations)"""
        # Try to use this user's cart first, otherwise pick a random one
        cart_id = self.my_cart_id

        if not cart_id and cart_ids:
            cart_id = random.choice(cart_ids)

        if not cart_id:
            # No carts available, create one first
            self.create_cart()
            return

        with self.client.get(
            f"/shopping-carts/{cart_id}",
            catch_response=True,
            name="GET /shopping-carts/{id} (retrieve)"
        ) as response:
            if response.status_code == 200:
                try:
                    data = response.json()
                    if "cart" in data and "items" in data:
                        response.success()
                    else:
                        response.failure("Invalid response structure")
                except json.JSONDecodeError:
                    response.failure("Invalid JSON response")
            elif response.status_code == 404:
                # Cart not found, remove from our list
                if cart_id in cart_ids:
                    cart_ids.remove(cart_id)
                if self.my_cart_id == cart_id:
                    self.my_cart_id = None
                response.failure("Cart not found")
            else:
                response.failure(f"Failed with status {response.status_code}")


# Event hooks for initialization and summary
@events.test_start.add_listener
def on_test_start(environment, **kwargs):
    """Called once before the test starts"""
    print(f"\n{'='*60}")
    print(f"Starting Shopping Cart Load Test")
    print(f"Backend: {MODE}")
    print(f"Host: {environment.host}")
    print(f"{'='*60}\n")


@events.test_stop.add_listener
def on_test_stop(environment, **kwargs):
    """Called once after the test stops"""
    print(f"\n{'='*60}")
    print(f"Test Complete!")
    print(f"Total carts created: {len(cart_ids)}")
    print(f"{'='*60}\n")
